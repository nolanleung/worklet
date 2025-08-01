# Agent Guidelines for Worklet

## Build & Test Commands
- **Build**: `go build -o worklet .` (creates binary in root)
- **Test**: `go test ./...` (run all tests)
- **Single test**: `go test ./internal/docker -run TestCopyWorkspace`
- **Lint**: Use `gofmt -s -w .` and `go vet ./...`

## Code Style & Conventions
- **Language**: Go 1.23.5 with standard library conventions
- **Imports**: Group standard, third-party, then local imports with blank lines
- **Naming**: Use camelCase for unexported, PascalCase for exported identifiers
- **Structs**: Use JSON tags for config structs (e.g., `json:"name"`)
- **Error handling**: Always check errors, wrap with `fmt.Errorf("context: %w", err)`
- **Comments**: Use godoc format for exported functions/types
- **Files**: Use snake_case for test files (e.g., `docker_test.go`)

## Project Structure
- `cmd/worklet/`: CLI commands using cobra framework
- `internal/`: Private packages (config, docker, nginx)
- `pkg/`: Public packages (daemon, terminal)
- `scripts/`: Shell scripts and utilities

## Dependencies
- CLI: github.com/spf13/cobra
- Config: github.com/tidwall/jsonc for JSONC parsing
- Docker: github.com/docker/docker for Docker API
- WebSocket: github.com/gorilla/websocket for terminal

## Testing
- Use standard `testing` package
- Create temp directories with `os.MkdirTemp` for file operations
- Clean up with `defer os.RemoveAll(tempDir)`