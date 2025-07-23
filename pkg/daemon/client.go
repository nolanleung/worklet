package daemon

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

// Client represents a client connection to the worklet daemon
type Client struct {
	socketPath string
	conn       net.Conn
	encoder    *json.Encoder
	decoder    *json.Decoder
	timeout    time.Duration
}

// NewClient creates a new daemon client
func NewClient(socketPath string) *Client {
	return &Client{
		socketPath: socketPath,
		timeout:    10 * time.Second,
	}
}

// GetDefaultSocketPath returns the default socket path
func GetDefaultSocketPath() string {
	// Check if running as root
	if os.Geteuid() == 0 {
		return "/var/run/worklet.sock"
	}
	
	// Use user's home directory for non-root
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "/tmp/worklet.sock"
	}
	
	return filepath.Join(homeDir, ".worklet", "worklet.sock")
}

// Connect establishes a connection to the daemon
func (c *Client) Connect() error {
	conn, err := net.Dial("unix", c.socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	
	c.conn = conn
	c.encoder = json.NewEncoder(conn)
	c.decoder = json.NewDecoder(conn)
	
	return nil
}

// Close closes the client connection
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// RegisterFork registers a new fork with the daemon
func (c *Client) RegisterFork(ctx context.Context, req RegisterForkRequest) error {
	msg := Message{
		Type:    MsgRegisterFork,
		ID:      uuid.New().String(),
		Payload: mustMarshal(req),
	}
	
	resp, err := c.sendRequest(ctx, &msg)
	if err != nil {
		return err
	}
	
	if resp.Type == MsgError {
		var errResp ErrorResponse
		json.Unmarshal(resp.Payload, &errResp)
		return fmt.Errorf("daemon error: %s", errResp.Error)
	}
	
	return nil
}

// UnregisterFork removes a fork registration from the daemon
func (c *Client) UnregisterFork(ctx context.Context, forkID string) error {
	req := UnregisterForkRequest{
		ForkID: forkID,
	}
	
	msg := Message{
		Type:    MsgUnregisterFork,
		ID:      uuid.New().String(),
		Payload: mustMarshal(req),
	}
	
	resp, err := c.sendRequest(ctx, &msg)
	if err != nil {
		return err
	}
	
	if resp.Type == MsgError {
		var errResp ErrorResponse
		json.Unmarshal(resp.Payload, &errResp)
		return fmt.Errorf("daemon error: %s", errResp.Error)
	}
	
	return nil
}

// ListForks returns all registered forks
func (c *Client) ListForks(ctx context.Context) ([]ForkInfo, error) {
	msg := Message{
		Type: MsgListForks,
		ID:   uuid.New().String(),
	}
	
	resp, err := c.sendRequest(ctx, &msg)
	if err != nil {
		return nil, err
	}
	
	if resp.Type == MsgError {
		var errResp ErrorResponse
		json.Unmarshal(resp.Payload, &errResp)
		return nil, fmt.Errorf("daemon error: %s", errResp.Error)
	}
	
	var listResp ListForksResponse
	if err := json.Unmarshal(resp.Payload, &listResp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	
	return listResp.Forks, nil
}

// GetForkInfo returns information about a specific fork
func (c *Client) GetForkInfo(ctx context.Context, forkID string) (*ForkInfo, error) {
	req := GetForkInfoRequest{
		ForkID: forkID,
	}
	
	msg := Message{
		Type:    MsgGetForkInfo,
		ID:      uuid.New().String(),
		Payload: mustMarshal(req),
	}
	
	resp, err := c.sendRequest(ctx, &msg)
	if err != nil {
		return nil, err
	}
	
	if resp.Type == MsgError {
		var errResp ErrorResponse
		json.Unmarshal(resp.Payload, &errResp)
		return nil, fmt.Errorf("daemon error: %s", errResp.Error)
	}
	
	var forkInfo ForkInfo
	if err := json.Unmarshal(resp.Payload, &forkInfo); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	
	return &forkInfo, nil
}

// HealthCheck checks if the daemon is running
func (c *Client) HealthCheck(ctx context.Context) error {
	msg := Message{
		Type: MsgHealthCheck,
		ID:   uuid.New().String(),
	}
	
	resp, err := c.sendRequest(ctx, &msg)
	if err != nil {
		return err
	}
	
	if resp.Type != MsgSuccess {
		return fmt.Errorf("unexpected response type: %s", resp.Type)
	}
	
	return nil
}

// sendRequest sends a request and waits for a response
func (c *Client) sendRequest(ctx context.Context, msg *Message) (*Message, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}
	
	// Set deadline on connection
	deadline, ok := ctx.Deadline()
	if !ok {
		deadline = time.Now().Add(c.timeout)
	}
	c.conn.SetDeadline(deadline)
	
	// Send request
	if err := c.encoder.Encode(msg); err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	
	// Wait for response
	var resp Message
	if err := c.decoder.Decode(&resp); err != nil {
		return nil, fmt.Errorf("failed to receive response: %w", err)
	}
	
	// Verify response ID matches request
	if resp.ID != msg.ID {
		return nil, fmt.Errorf("response ID mismatch")
	}
	
	return &resp, nil
}

// IsDaemonRunning checks if the daemon is running
func IsDaemonRunning(socketPath string) bool {
	client := NewClient(socketPath)
	if err := client.Connect(); err != nil {
		return false
	}
	defer client.Close()
	
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	
	return client.HealthCheck(ctx) == nil
}