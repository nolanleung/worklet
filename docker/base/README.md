# Worklet Docker Image with Claude Code

This directory contains a Dockerfile that builds a Docker-in-Docker image with Claude Code pre-installed.

## Building the Image

```bash
# From the worklet root directory
docker build -t worklet/dind-claude-code:latest docker/with-claude-code/

# Or with a custom tag
docker build -t myregistry/worklet-claude:v1.0 docker/with-claude-code/
```

## Using the Image

1. Build the image locally or push to your registry
2. Update your `.worklet.jsonc` to use the custom image:

```jsonc
{
  "run": {
    "image": "worklet/base:latest",
    "isolation": "full",
    "privileged": true,
    "environment": {
      "DOCKER_TLS_CERTDIR": ""
    }
  }
}
```

3. Link your Claude authentication (if not already done):

```bash
worklet link claude
```

4. Run your worklet fork:

```bash
worklet switch my-fork
# Claude is now available in the container
/workspace # claude --help
```

## What's Included

- Docker-in-Docker functionality (from `docker:dind`)
- Claude Code CLI (`/usr/local/bin/claude`)
- Essential dependencies for Claude Code
- Support for mounting Claude authentication via `worklet link claude`

## Notes

- The image is based on Alpine Linux for a smaller size
- Claude authentication must be linked separately using `worklet link claude`
- The worklet entrypoint script is mounted at runtime, preserving all worklet functionality
