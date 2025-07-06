# Worklet

A powerful tool for creating isolated development environments. Fork your repository, test changes in Docker containers, and seamlessly integrate results back into your Git workflow.

## Why Worklet?

- **Isolated Testing**: Test changes without affecting your main repository
- **Docker-in-Docker**: Run complex Docker workflows in complete isolation
- **Git Integration**: Commit and push changes directly from forks
- **Zero Configuration**: Works out of the box with sensible defaults
- **Fast Switching**: Jump between different test environments instantly

## Quick Start

```bash
# Install worklet
go install github.com/nolanleung/worklet@latest

# Initialize configuration  
worklet init

# Fork your current repository
worklet fork

# Switch to the fork and run in Docker
worklet switch 1

# You're now in an isolated Docker environment!
# Make changes, run tests, experiment freely

# When done, exit and commit your changes
exit
cd ~/.worklet/forks/fork-*
git checkout -b feature/tested-change
git add . && git commit -m "Tested feature"
git push origin feature/tested-change
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
Create a complete copy of your repository with one command. Each fork is isolated, allowing you to experiment without fear.

### 🐳 **True Docker-in-Docker Support**
- **Full isolation mode** (default): Runs a separate Docker daemon inside the container
- **Shared mode**: Uses host Docker daemon for resource efficiency
- Perfect for testing Docker Compose setups, building images, or running containerized tests

### 📊 **Visual Progress Tracking**
Watch your fork creation progress with a real-time progress bar showing files copied and size.

### 🔧 **Comprehensive Fork Management**
- List all forks with sizes and creation times
- Remove specific forks or clean up old ones
- Switch between forks instantly
- Monitor disk usage easily

### 📝 **Git Workflow Integration**
Forks include the complete `.git` directory, enabling you to:
- Create feature branches from experimental work
- Commit changes made during testing
- Push directly to remote repositories
- Maintain full Git history

### 🔌 **Self-Contained Binary**
The worklet binary includes all necessary scripts. No external dependencies beyond Docker.

### 🚀 **Init Scripts**
Run initialization commands automatically when containers start - perfect for installing dependencies, setting up tools, or configuring the environment.

## Configuration

Create a `.worklet.jsonc` file in your repository root:

```jsonc
{
  "fork": {
    "name": "my-project",
    "description": "Testing new features",
    "includeGit": true,     // Include .git directory (default: true)
    "exclude": [            // Patterns to exclude from fork
      "node_modules",
      "*.log",
      ".DS_Store",
      "dist",
      "build"
    ]
  },
  "run": {
    "image": "docker:dind",              // Docker image to use
    "isolation": "full",                 // "full" or "shared"
    "command": ["sh"],                   // Command to run
    "initScript": [                      // Commands to run on container start
      "apt-get update",
      "apt-get install -y nodejs npm",
      "npm install -g yarn"
    ],
    "environment": {                     // Environment variables
      "DOCKER_TLS_CERTDIR": "",
      "DOCKER_DRIVER": "overlay2"
    },
    "volumes": [],                       // Additional volume mounts
    "privileged": true                   // Required for Docker-in-Docker
  }
}
```

## Commands

### `worklet init`
Initialize a new `.worklet.jsonc` configuration file.

```bash
worklet init                    # Create config with defaults
worklet init --minimal          # Create minimal config
worklet init --force            # Overwrite existing config
```

Creates a `.worklet.jsonc` configuration file with sensible defaults.

### `worklet fork`
Create a fork of the current repository.

```bash
worklet fork
# Output: Fork created at: /Users/you/.worklet/forks/fork-abc123
```

### `worklet run`
Run the current directory in a Docker container.

```bash
worklet run
# Starts Docker container with your configuration
```

### `worklet switch [fork]`
Switch to a fork and run it in Docker immediately.

```bash
worklet switch                  # Interactive selection
worklet switch 1                # By index (from list)
worklet switch fork-abc123      # By fork ID
worklet switch my-project       # By name
worklet switch --print-path 1   # Just print path (for scripts)
```

### `worklet list`
List all forks with details.

```bash
worklet list                    # Table view
worklet list --json            # JSON output
worklet list --verbose         # Detailed information
```

Example output:
```
FORK ID         NAME           CREATED        SIZE      SOURCE
fork-abc123     my-project     2 hours ago    125.3 MB  /Users/you/project
fork-def456     experiment     1 day ago      89.2 MB   /Users/you/project

Total: 2 forks (214.5 MB)
```

### `worklet remove [fork-ids...]`
Remove specific forks.

```bash
worklet remove fork-abc123              # Single fork
worklet remove fork-abc fork-def       # Multiple forks
worklet remove fork-abc123 --force     # Skip confirmation
```

### `worklet clean`
Clean up forks.

```bash
worklet clean                    # Remove all forks
worklet clean --older-than 7     # Remove forks older than 7 days
worklet clean --dry-run          # Preview what would be deleted
worklet clean --force            # Skip confirmation
```

## Workflows

### Testing a Risky Change

```bash
# 1. Fork your repository
$ worklet fork
Fork created at: /Users/you/.worklet/forks/fork-abc123

# 2. Switch to the fork (runs in Docker)
$ worklet switch fork-abc123
Starting Docker daemon in full isolation mode...

# 3. Make your risky changes
/workspace # rm -rf old-module/
/workspace # docker-compose up -d
/workspace # npm test

# 4. If successful, commit from the fork
/workspace # exit
$ cd ~/.worklet/forks/fork-abc123
$ git add -A
$ git commit -m "Removed old module, all tests pass"
$ git push origin feature/remove-old-module
```

### Testing Docker Compose Setups

```bash
# Your docker-compose.yml is completely isolated
$ worklet switch my-fork
/workspace # docker-compose up -d
/workspace # docker ps  # Only shows containers in this isolated environment
/workspace # docker-compose down
```

### Parallel Testing

```bash
# Fork multiple times for parallel experiments
$ worklet fork && worklet fork && worklet fork

# List your forks
$ worklet list
FORK ID         NAME           CREATED
fork-abc123     my-project     just now
fork-def456     my-project     just now  
fork-ghi789     my-project     just now

# Run different tests in each fork
$ worklet switch 1  # Test configuration A
$ worklet switch 2  # Test configuration B
$ worklet switch 3  # Test configuration C
```

### Using Init Scripts

Configure automatic dependency installation:

```jsonc
// .worklet.jsonc
{
  "run": {
    "image": "node:18",
    "initScript": [
      "npm install",                    // Install project dependencies
      "npm install -g typescript",      // Install global tools
      "npx playwright install"          // Set up test browsers
    ]
  }
}
```

Now when you run:
```bash
$ worklet switch my-fork
Running initialization script...
npm install
✓ Dependencies installed
npm install -g typescript
✓ TypeScript installed globally
npx playwright install
✓ Playwright browsers installed

# Your environment is ready to use!
/workspace #
```

## Shell Integration

### Quick Fork Directory Access

Add to your `~/.bashrc` or `~/.zshrc`:

```bash
# Change directory to a fork
wcd() {
    local path=$(worklet switch --print-path "$@")
    if [ $? -eq 0 ] && [ -n "$path" ]; then
        cd "$path"
        echo "Changed to: $path"
    fi
}

# List and cd to a fork
wl() {
    worklet list
    echo -n "Enter fork number: "
    read num
    wcd $num
}
```

## Tips and Best Practices

1. **Regular Cleanup**: Use `worklet clean --older-than 7` weekly to manage disk space
2. **Naming Forks**: Use descriptive names in `.worklet.jsonc` for easier identification
3. **Git Strategy**: Create feature branches directly from forks after successful testing
4. **Disk Space**: Monitor with `worklet list` - fork sizes include the full `.git` directory
5. **Performance**: Exclude large directories (like `node_modules`) for faster forking

## Troubleshooting

### Docker Permission Denied
Ensure your user is in the docker group:
```bash
sudo usermod -aG docker $USER
# Log out and back in
```

### Fork Creation Fails
Check disk space and permissions:
```bash
df -h ~/.worklet
ls -la ~/.worklet/forks
```

### Docker-in-Docker Issues
Ensure you're using `"isolation": "full"` and `"privileged": true` in your configuration.

## Requirements

- Go 1.21 or later (for building from source)
- Docker installed and running
- Sufficient disk space for forks

## License

MIT License - see LICENSE file for details.

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.