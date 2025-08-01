package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type SessionState int

const (
	SessionStateActive SessionState = iota
	SessionStateDetached
	SessionStateTerminated
)

type Session struct {
	ID           string
	ForkID       string
	ContainerID  string
	conns        []*websocket.Conn // Support multiple connections
	connMu       sync.RWMutex      // Protect concurrent access to conns
	docker       *client.Client
	execID       string
	hijacked     types.HijackedResponse
	ctx          context.Context
	cancel       context.CancelFunc
	state        SessionState
	stateMu      sync.RWMutex
	lastActivity time.Time
	outputBuffer []byte // Buffer to store recent output for replay
	bufferMu     sync.RWMutex
}

type SessionManager struct {
	sessions      map[string]*Session // By session ID
	forkSessions  map[string]*Session // By fork ID
	mu            sync.RWMutex
	cleanupTicker *time.Ticker
	cleanupDone   chan bool
}

func NewSessionManager() *SessionManager {
	sm := &SessionManager{
		sessions:     make(map[string]*Session),
		forkSessions: make(map[string]*Session),
		cleanupDone:  make(chan bool),
	}

	// Start cleanup goroutine
	sm.cleanupTicker = time.NewTicker(5 * time.Minute)
	go sm.cleanupRoutine()

	return sm
}

func (sm *SessionManager) CreateOrAttachSession(forkID string, conn *websocket.Conn) (*Session, error) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	// Check if session exists for this fork
	if existingSession, exists := sm.forkSessions[forkID]; exists {
		// Attach to existing session
		existingSession.stateMu.Lock()
		if existingSession.state != SessionStateTerminated {
			existingSession.state = SessionStateActive
			existingSession.lastActivity = time.Now()
			existingSession.stateMu.Unlock()

			// Add connection to session
			existingSession.AddConnection(conn)

			// Send buffered output to new connection
			existingSession.ReplayBuffer(conn)

			return existingSession, nil
		}
		existingSession.stateMu.Unlock()

		// Session is terminated, remove it
		delete(sm.sessions, existingSession.ID)
		delete(sm.forkSessions, forkID)
	}

	// Create new session
	containerID, err := GetContainerID(forkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get container ID: %w", err)
	}

	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := &Session{
		ID:           uuid.New().String(),
		ForkID:       forkID,
		ContainerID:  containerID,
		conns:        []*websocket.Conn{conn},
		docker:       dockerClient,
		ctx:          ctx,
		cancel:       cancel,
		state:        SessionStateActive,
		lastActivity: time.Now(),
		outputBuffer: make([]byte, 0, 64*1024), // 64KB buffer
	}

	sm.sessions[session.ID] = session
	sm.forkSessions[forkID] = session

	return session, nil
}

func (sm *SessionManager) DetachSession(sessionID string, conn *websocket.Conn) {
	sm.mu.RLock()
	session, ok := sm.sessions[sessionID]
	sm.mu.RUnlock()

	if !ok {
		return
	}

	// Remove connection from session
	session.RemoveConnection(conn)

	// Check if session has any active connections
	session.connMu.RLock()
	hasConnections := len(session.conns) > 0
	session.connMu.RUnlock()

	if !hasConnections {
		// No more connections, mark as detached
		session.stateMu.Lock()
		session.state = SessionStateDetached
		session.lastActivity = time.Now()
		session.stateMu.Unlock()
	}
}

func (sm *SessionManager) TerminateSession(sessionID string) {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	if session, ok := sm.sessions[sessionID]; ok {
		session.stateMu.Lock()
		session.state = SessionStateTerminated
		session.stateMu.Unlock()

		session.Close()
		delete(sm.sessions, sessionID)
		delete(sm.forkSessions, session.ForkID)
	}
}

func (s *Session) Start() error {
	// Only create exec if this is a new session
	if s.execID == "" {
		execConfig := container.ExecOptions{
			AttachStdin:  true,
			AttachStdout: true,
			AttachStderr: true,
			Tty:          true,
			Cmd:          []string{"/bin/sh"},
			ConsoleSize:  &[2]uint{40, 140}, // height, width
		}

		execResp, err := s.docker.ContainerExecCreate(s.ctx, s.ContainerID, execConfig)
		if err != nil {
			return fmt.Errorf("failed to create exec: %w", err)
		}
		s.execID = execResp.ID

		// Attach to exec
		attachResp, err := s.docker.ContainerExecAttach(s.ctx, s.execID, container.ExecStartOptions{
			Tty:         true,
			ConsoleSize: &[2]uint{40, 140}, // height, width
		})
		if err != nil {
			return fmt.Errorf("failed to attach to exec: %w", err)
		}
		s.hijacked = attachResp

		// Start goroutine to read from container
		go s.readFromContainer()
	}

	// Start goroutine for this connection's input
	s.connMu.RLock()
	if len(s.conns) > 0 {
		go s.readFromWebSocket(s.conns[len(s.conns)-1]) // Read from the latest connection
	}
	s.connMu.RUnlock()

	// Wait for context cancellation
	<-s.ctx.Done()
	return nil
}

func (s *Session) readFromContainer() {
	buf := make([]byte, 1024)
	for {
		n, err := s.hijacked.Reader.Read(buf)
		if err != nil {
			if err != io.EOF {
				log.Printf("Error reading from container: %v", err)
			}
			s.cancel()
			return
		}

		if n > 0 {
			data := buf[:n]
			// Append to buffer for replay
			s.appendToBuffer(data)
			// Broadcast to all connections
			s.broadcastToConnections(websocket.BinaryMessage, data)
		}
	}
}

func (s *Session) readFromWebSocket(conn *websocket.Conn) {
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			// Remove this connection
			s.RemoveConnection(conn)
			return
		}

		// Update activity timestamp
		s.stateMu.Lock()
		s.lastActivity = time.Now()
		s.stateMu.Unlock()

		// Handle special messages
		if len(message) > 0 && message[0] == '{' {
			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err == nil {
				if msg["type"] == "resize" {
					// Handle resize
					if rows, ok := msg["rows"].(float64); ok {
						if cols, ok := msg["cols"].(float64); ok {
							if err := s.resize(int(rows), int(cols)); err != nil {
								log.Printf("Failed to resize terminal: %v", err)
							}
						}
					}
					continue
				}
			}
		}

		// Write to container
		if s.hijacked.Conn != nil {
			if _, err := s.hijacked.Conn.Write(message); err != nil {
				log.Printf("Error writing to container: %v", err)
				// Don't cancel the entire session, just this connection
				s.RemoveConnection(conn)
				return
			}
		}
	}
}

func (s *Session) resize(rows, cols int) error {
	return s.docker.ContainerExecResize(s.ctx, s.execID, container.ResizeOptions{
		Height: uint(rows),
		Width:  uint(cols),
	})
}

func (s *Session) AddConnection(conn *websocket.Conn) {
	s.connMu.Lock()
	s.conns = append(s.conns, conn)
	s.connMu.Unlock()

	// Start reading from this new connection
	go s.readFromWebSocket(conn)
}

func (s *Session) RemoveConnection(conn *websocket.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()

	for i, c := range s.conns {
		if c == conn {
			// Remove connection from slice
			s.conns = append(s.conns[:i], s.conns[i+1:]...)
			break
		}
	}
}

func (s *Session) ReplayBuffer(conn *websocket.Conn) {
	s.bufferMu.RLock()
	defer s.bufferMu.RUnlock()

	if len(s.outputBuffer) > 0 {
		// Send buffered output to new connection
		conn.WriteMessage(websocket.BinaryMessage, s.outputBuffer)
	}
}

func (s *Session) appendToBuffer(data []byte) {
	s.bufferMu.Lock()
	defer s.bufferMu.Unlock()

	// Append to buffer
	s.outputBuffer = append(s.outputBuffer, data...)

	// Keep only last 64KB
	if len(s.outputBuffer) > 64*1024 {
		s.outputBuffer = s.outputBuffer[len(s.outputBuffer)-64*1024:]
	}
}

func (s *Session) broadcastToConnections(messageType int, data []byte) {
	s.connMu.RLock()
	connections := make([]*websocket.Conn, len(s.conns))
	copy(connections, s.conns)
	s.connMu.RUnlock()

	// Send to all connections
	for _, conn := range connections {
		if err := conn.WriteMessage(messageType, data); err != nil {
			log.Printf("Error writing to WebSocket: %v", err)
			// Connection is probably dead, it will be removed when detected
		}
	}
}

func (s *Session) Close() {
	s.cancel()
	if s.hijacked.Conn != nil {
		s.hijacked.Close()
	}

	// Close all connections
	s.connMu.Lock()
	for _, conn := range s.conns {
		conn.Close()
	}
	s.conns = nil
	s.connMu.Unlock()

	if s.docker != nil {
		s.docker.Close()
	}
}

func (sm *SessionManager) cleanupRoutine() {
	for {
		select {
		case <-sm.cleanupTicker.C:
			sm.cleanupInactiveSessions()
		case <-sm.cleanupDone:
			return
		}
	}
}

func (sm *SessionManager) cleanupInactiveSessions() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	now := time.Now()
	timeout := 30 * time.Minute // Sessions timeout after 30 minutes of inactivity

	for id, session := range sm.sessions {
		session.stateMu.RLock()
		state := session.state
		lastActivity := session.lastActivity
		session.stateMu.RUnlock()

		if state == SessionStateDetached && now.Sub(lastActivity) > timeout {
			log.Printf("Cleaning up inactive session %s for fork %s", id, session.ForkID)
			session.Close()
			delete(sm.sessions, id)
			delete(sm.forkSessions, session.ForkID)
		}
	}
}

func (sm *SessionManager) Stop() {
	close(sm.cleanupDone)
	sm.cleanupTicker.Stop()

	// Close all sessions
	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, session := range sm.sessions {
		session.Close()
	}
}
