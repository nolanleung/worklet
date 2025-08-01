package daemon

import (
	"encoding/json"
	"time"
)

// MessageType represents the type of message sent between client and daemon
type MessageType string

const (
	// Client -> Daemon messages
	MsgRegisterFork   MessageType = "REGISTER_FORK"
	MsgUnregisterFork MessageType = "UNREGISTER_FORK"
	MsgListForks      MessageType = "LIST_FORKS"
	MsgGetForkInfo    MessageType = "GET_FORK_INFO"
	MsgProxyRegister  MessageType = "PROXY_REGISTER"
	MsgHealthCheck    MessageType = "HEALTH_CHECK"
	MsgRefreshFork    MessageType = "REFRESH_FORK"
	MsgRefreshAll     MessageType = "REFRESH_ALL"
	MsgRequestForkID  MessageType = "REQUEST_FORK_ID"
	
	// Daemon -> Client responses
	MsgSuccess        MessageType = "SUCCESS"
	MsgError          MessageType = "ERROR"
	MsgForkList       MessageType = "FORK_LIST"
	MsgForkInfo       MessageType = "FORK_INFO"
	MsgForkID         MessageType = "FORK_ID"
)

// Message represents a message between client and daemon
type Message struct {
	Type    MessageType     `json:"type"`
	ID      string          `json:"id,omitempty"`      // Request ID for correlation
	Payload json.RawMessage `json:"payload,omitempty"`
}

// RegisterForkRequest is sent when a new fork is created
type RegisterForkRequest struct {
	ForkID      string            `json:"fork_id"`
	ProjectName string            `json:"project_name"`
	ContainerID string            `json:"container_id,omitempty"`
	WorkDir     string            `json:"work_dir"`
	Services    []ServiceInfo     `json:"services,omitempty"`
	Metadata    map[string]string `json:"metadata,omitempty"`
}

// ServiceInfo describes a service exposed by a fork
type ServiceInfo struct {
	Name      string `json:"name"`
	Port      int    `json:"port"`
	Subdomain string `json:"subdomain"`
}

// UnregisterForkRequest is sent when a fork is being removed
type UnregisterForkRequest struct {
	ForkID string `json:"fork_id"`
}

// GetForkInfoRequest requests information about a specific fork
type GetForkInfoRequest struct {
	ForkID string `json:"fork_id"`
}

// ForkInfo contains information about a registered fork
type ForkInfo struct {
	ForkID       string            `json:"fork_id"`
	ProjectName  string            `json:"project_name"`
	ContainerID  string            `json:"container_id,omitempty"`
	WorkDir      string            `json:"work_dir"`
	Services     []ServiceInfo     `json:"services,omitempty"`
	Metadata     map[string]string `json:"metadata,omitempty"`
	RegisteredAt time.Time         `json:"registered_at"`
	LastSeenAt   time.Time         `json:"last_seen_at"`
}

// ListForksResponse contains a list of all registered forks
type ListForksResponse struct {
	Forks []ForkInfo `json:"forks"`
}

// ErrorResponse is sent when an error occurs
type ErrorResponse struct {
	Error string `json:"error"`
	Code  string `json:"code,omitempty"`
}

// SuccessResponse is sent for successful operations
type SuccessResponse struct {
	Message string `json:"message,omitempty"`
}

// RefreshForkRequest is sent to refresh a specific fork's information
type RefreshForkRequest struct {
	ForkID string `json:"fork_id"`
}

// RefreshAllRequest is sent to refresh all forks
type RefreshAllRequest struct {
	// No fields needed for refresh all
}

// RequestForkIDResponse contains the next available fork ID
type RequestForkIDResponse struct {
	ForkID string `json:"fork_id"`
}