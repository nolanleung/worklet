package terminal

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/websocket"
)

//go:embed web/*
var webAssets embed.FS

type Server struct {
	port        int
	manager     *SessionManager
	corsOrigin  string
}

func NewServer(port int) *Server {
	return &Server{
		port:       port,
		manager:    NewSessionManager(),
		corsOrigin: "*", // Default to allow all origins
	}
}

// SetCORSOrigin sets the allowed CORS origin
func (s *Server) SetCORSOrigin(origin string) {
	s.corsOrigin = origin
}

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

// corsMiddleware adds CORS headers to HTTP responses
func (s *Server) corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Set CORS headers
		w.Header().Set("Access-Control-Allow-Origin", s.corsOrigin)
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Max-Age", "86400") // 24 hours

		// Handle preflight requests
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}

		// Call the next handler
		next(w, r)
	}
}

func (s *Server) Start() error {
	mux := http.NewServeMux()

	// Serve static files
	mux.Handle("/", http.FileServer(http.FS(webAssets)))

	// API endpoints with CORS middleware
	mux.HandleFunc("/api/forks", s.corsMiddleware(s.handleForks))
	mux.HandleFunc("/terminal/", s.handleWebSocket)

	addr := fmt.Sprintf(":%d", s.port)
	log.Printf("Terminal server starting on http://localhost%s", addr)
	return http.ListenAndServe(addr, mux)
}

func (s *Server) handleForks(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	forks, err := ListForks()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(forks)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	// Extract fork ID from path
	path := strings.TrimPrefix(r.URL.Path, "/terminal/")
	forkID := strings.TrimSuffix(path, "/")

	if forkID == "" {
		http.Error(w, "Fork ID required", http.StatusBadRequest)
		return
	}

	// Upgrade connection to WebSocket
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("WebSocket upgrade error: %v", err)
		return
	}
	defer conn.Close()

	// Create terminal session
	session, err := s.manager.CreateSession(forkID, conn)
	if err != nil {
		log.Printf("Failed to create session: %v", err)
		conn.WriteJSON(map[string]string{"error": err.Error()})
		return
	}
	defer s.manager.RemoveSession(session.ID)

	// Start session
	session.Start()
}