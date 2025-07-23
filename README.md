# Worklet

A powerful tool for running projects in isolated Docker containers with Docker-in-Docker support.

## Why Worklet?

- **Isolated Environments**: Run your project in clean Docker containers
- **Docker-in-Docker**: Run complex Docker workflows in complete isolation
- **Service Discovery**: Automatic subdomain routing for multi-service projects
- **Zero Configuration**: Works out of the box with sensible defaults
- **Real-time Development**: Mount mode for live code changes

## Quick Start

```bash
# Install worklet
go install github.com/nolanleung/worklet@latest

# Initialize configuration  
worklet init

# Run your project in an isolated container
worklet run

# Or run with your local directory mounted
worklet run --mount

# Run a specific command
worklet run npm test
```

## Installation

### From Source

```bash
go install github.com/nolanleung/worklet@latest
```

### Manual Build

```bash
git clone https://github.com/nolanleung/worklet
cd worklet
go build -o worklet .
sudo mv worklet /usr/local/bin/
```

## Features

### 🚀 **Instant Isolated Environments**
Run your project in a clean Docker container with one command. Each session is isolated, allowing you to experiment without fear.

### 🐳 **True Docker-in-Docker Support**
- **Full isolation mode** (default): Runs a separate Docker daemon inside the container
- **Shared mode**: Uses host Docker daemon for resource efficiency
- Perfect for testing Docker Compose setups, building images, or running containerized tests

### 🔧 **Flexible Run Modes**
- **Isolated mode** (default): Creates a persistent isolated environment
- **Mount mode** (`--mount`): Mounts your current directory for real-time development
- **Temporary mode** (`--temp`): Creates a temporary environment that auto-cleans up

### 🌐 **Service Discovery & Routing**
- Automatic subdomain routing for multi-service projects
- Access services via `service.project-name.worklet.sh`
- Built-in proxy server for local development

### 🔌 **Self-Contained Binary**
The worklet binary includes all necessary scripts. No external dependencies beyond Docker.

### 🚀 **Init Scripts**
Run initialization commands automatically when containers start - perfect for installing dependencies, setting up tools, or configuring the environment.


## Configuration

Create a `.worklet.jsonc` file in your repository root:

```jsonc
{
  "name": "my-project",     // Project name for container naming
  "run": {
    "image": "docker:dind",          // Base Docker image (default: docker:dind)
    "privileged": true,              // Run with Docker-in-Docker
    "isolation": "full",             // "full" for DinD, "shared" for socket mount
    "command": ["/bin/sh"],          // Default command (optional)
    "environment": {                 // Environment variables
      "NODE_ENV": "development",
      "DEBUG": "true"
    },
    "volumes": [                     // Additional volume mounts
      "/var/lib/mysql"
    ],
    "initScript": [                  // Commands to run on container start
      "apk add --no-cache nodejs npm python3",
      "npm install -g pnpm",
      "echo 'Welcome to Worklet!'"
    ],
    "credentials": {
      "claude": true                 // Mount Claude credentials if available
    }
  },
  "services": [                      // Services exposed by your project
    {
      "name": "web",
      "port": 3000,
      "subdomain": "app"             // Access via app.my-project.worklet.sh
    },
    {
      "name": "api", 
      "port": 3001,
      "subdomain": "api"             // Access via api.my-project.worklet.sh
    }
  ]
}
```

## Command Reference

### `worklet init`
Initialize a new `.worklet.jsonc` configuration file.

```bash
worklet init
# Creates .worklet.jsonc with default configuration
```

### `worklet run`
Run your project in a Docker container.

```bash
worklet run                      # Run in isolated environment
worklet run --mount              # Run with current directory mounted
worklet run --temp               # Run in temporary environment
worklet run npm test             # Run specific command
worklet run --mount npm start    # Run with mount and command
```

### `worklet terminal`
Start a web-based terminal server for browser-based access to containers.

```bash
worklet terminal                 # Start terminal server on port 7681
worklet terminal --port 8080     # Use custom port
worklet terminal --proxy          # Enable service proxy
```

Features:
- Browser-based terminal with full TTY support
- Automatic container discovery
- Service proxy for accessing project services via subdomains

### `worklet daemon`
Manage the worklet daemon for service discovery and proxy routing.

```bash
worklet daemon start        # Start the daemon
worklet daemon stop         # Stop the daemon  
worklet daemon status       # Check daemon status
```

The daemon:
- Manages session registrations via Unix socket at `~/.worklet/worklet.sock`
- Enables automatic service discovery
- Persists session state across daemon restarts

## Configuration Examples

### Basic Node.js Project

```jsonc
{
  "name": "my-node-app",
  "run": {
    "image": "node:18",
    "initScript": [
      "npm install"
    ]
  },
  "services": [
    {
      "name": "app",
      "port": 3000
    }
  ]
}
```

### Python Development Environment

```jsonc
{
  "name": "python-ml",
  "run": {
    "image": "python:3.11",
    "environment": {
      "PYTHONUNBUFFERED": "1"
    },
    "initScript": [
      "pip install -r requirements.txt",
      "python -m nltk.downloader punkt"
    ]
  }
}
```

### Multi-Service Application

```jsonc
{
  "name": "microservices",
  "run": {
    "image": "docker:dind",
    "isolation": "full",
    "initScript": [
      "apk add --no-cache docker-compose",
      "docker-compose up -d"
    ]
  },
  "services": [
    {
      "name": "frontend",
      "port": 3000,
      "subdomain": "app"
    },
    {
      "name": "api", 
      "port": 8080,
      "subdomain": "api"
    },
    {
      "name": "admin",
      "port": 3001,
      "subdomain": "admin"
    }
  ]
}
```

## Workflows

### Development Workflow

```bash
# Start development with live reload
$ worklet run --mount npm run dev

# Your app is now running with:
# - Live code reloading
# - Full Docker-in-Docker support
# - Automatic service discovery

# Access your services:
# - http://app.my-project-12345.worklet.sh
# - http://api.my-project-12345.worklet.sh
```

### Testing Workflow

```bash
# Run tests in isolated environment
$ worklet run npm test

# Run integration tests with Docker Compose
$ worklet run docker-compose run tests

# Run tests in temporary environment
$ worklet run --temp npm test
```

### CI/CD Simulation

```bash
# Test your CI pipeline locally
$ worklet run ./scripts/ci.sh

# Full isolation ensures no side effects
# See exactly what your CI server sees
```

## Tips & Tricks

### Shell Aliases

Add these to your shell configuration:

```bash
# Quick run commands
alias wr='worklet run'
alias wrm='worklet run --mount'
alias wrt='worklet run --temp'

# Terminal access
alias wt='worklet terminal'
```

### Best Practices

1. **Project Names**: Set meaningful names in `.worklet.jsonc` for easier identification
2. **Mount Mode**: Use `--mount` for development, default mode for testing
3. **Init Scripts**: Keep them minimal for faster container startup
4. **Services**: Define all exposed ports for automatic discovery

## Architecture

Worklet uses a client-server architecture:

- **CLI**: The main `worklet` command that users interact with
- **Daemon**: Background process for service discovery and session management  
- **Terminal Server**: Web-based terminal and proxy server
- **Docker Integration**: Manages containers with proper isolation

## Requirements

- Docker Desktop or Docker Engine
- macOS, Linux, or Windows with WSL2
- Go 1.21+ (for building from source)

## Troubleshooting

### Container Won't Start

Check Docker is running:
```bash
docker ps
```

### Permission Denied

Ensure your user is in the docker group:
```bash
sudo usermod -aG docker $USER
```

### Port Already in Use

Check what's using the port:
```bash
lsof -i :3000
```

## Contributing

Contributions are welcome! Please read our [Contributing Guide](CONTRIBUTING.md) for details.

## License

MIT License - see [LICENSE](LICENSE) file for details.