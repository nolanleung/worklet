package terminal

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"sync"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type Session struct {
	ID          string
	ForkID      string
	ContainerID string
	conn        *websocket.Conn
	docker      *client.Client
	execID      string
	hijacked    types.HijackedResponse
	ctx         context.Context
	cancel      context.CancelFunc
}

type SessionManager struct {
	sessions map[string]*Session
	mu       sync.RWMutex
}

func NewSessionManager() *SessionManager {
	return &SessionManager{
		sessions: make(map[string]*Session),
	}
}

func (sm *SessionManager) CreateSession(forkID string, conn *websocket.Conn) (*Session, error) {
	// Get container ID for fork
	containerID, err := GetContainerID(forkID)
	if err != nil {
		return nil, fmt.Errorf("failed to get container ID: %w", err)
	}

	// Create Docker client
	dockerClient, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, fmt.Errorf("failed to create Docker client: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	session := &Session{
		ID:          uuid.New().String(),
		ForkID:      forkID,
		ContainerID: containerID,
		conn:        conn,
		docker:      dockerClient,
		ctx:         ctx,
		cancel:      cancel,
	}

	sm.mu.Lock()
	sm.sessions[session.ID] = session
	sm.mu.Unlock()

	return session, nil
}

func (sm *SessionManager) RemoveSession(sessionID string) {
	sm.mu.Lock()
	if session, ok := sm.sessions[sessionID]; ok {
		session.Close()
		delete(sm.sessions, sessionID)
	}
	sm.mu.Unlock()
}

func (s *Session) Start() error {
	// Create exec instance
	execConfig := container.ExecOptions{
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
		Cmd:          []string{"/bin/sh"},
	}

	execResp, err := s.docker.ContainerExecCreate(s.ctx, s.ContainerID, execConfig)
	if err != nil {
		return fmt.Errorf("failed to create exec: %w", err)
	}
	s.execID = execResp.ID

	// Attach to exec
	attachResp, err := s.docker.ContainerExecAttach(s.ctx, s.execID, container.ExecStartOptions{
		Tty: true,
	})
	if err != nil {
		return fmt.Errorf("failed to attach to exec: %w", err)
	}
	s.hijacked = attachResp

	// Start goroutines for bidirectional communication
	go s.readFromContainer()
	go s.readFromWebSocket()

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
			if err := s.conn.WriteMessage(websocket.TextMessage, buf[:n]); err != nil {
				log.Printf("Error writing to WebSocket: %v", err)
				s.cancel()
				return
			}
		}
	}
}

func (s *Session) readFromWebSocket() {
	for {
		_, message, err := s.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("WebSocket error: %v", err)
			}
			s.cancel()
			return
		}

		// Handle special messages
		if len(message) > 0 && message[0] == '{' {
			// Might be a resize message
			var msg map[string]interface{}
			if err := json.Unmarshal(message, &msg); err == nil {
				if msg["type"] == "resize" {
					rows := int(msg["rows"].(float64))
					cols := int(msg["cols"].(float64))
					s.resize(rows, cols)
					continue
				}
			}
		}

		// Write to container
		if _, err := s.hijacked.Conn.Write(message); err != nil {
			log.Printf("Error writing to container: %v", err)
			s.cancel()
			return
		}
	}
}

func (s *Session) resize(rows, cols int) error {
	return s.docker.ContainerExecResize(s.ctx, s.execID, container.ResizeOptions{
		Height: uint(rows),
		Width:  uint(cols),
	})
}

func (s *Session) Close() {
	s.cancel()
	if s.hijacked.Conn != nil {
		s.hijacked.Close()
	}
	if s.conn != nil {
		s.conn.Close()
	}
	if s.docker != nil {
		s.docker.Close()
	}
}