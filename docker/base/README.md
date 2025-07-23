# Worklet Base Docker Image

This directory contains the Dockerfile for the worklet base image with Docker-in-Docker support.

## Building the Image

```bash
# From the worklet root directory
docker build -t worklet/base:latest docker/base/

# Or with a custom tag
docker build -t myregistry/worklet-base:v1.0 docker/base/
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

3. Configure Claude credentials (if needed):

```bash
worklet credentials claude
worklet link claude
```

4. Run your worklet environment:

```bash
worklet run
# Docker-in-Docker is now available in the container
/workspace # docker --version
```

## What's Included

- Docker-in-Docker functionality (from `docker:dind`)
- Essential build tools and utilities
- Support for multiple package managers (npm, pnpm, bun)
- Support for mounting Claude credentials when configured via `worklet link claude`

## Notes

- The image is based on Alpine Linux for a smaller size
- Claude authentication must be configured separately using `worklet credentials claude` and `worklet link claude`
- The worklet entrypoint script is mounted at runtime, preserving all worklet functionality