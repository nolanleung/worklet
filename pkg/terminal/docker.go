package terminal

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type ContainerInfo struct {
	ID          string `json:"id"`
	ContainerID string `json:"container_id"`
	Status      string `json:"status"`
}

// ListSessions returns a list of available worklet sessions
func ListSessions() ([]ContainerInfo, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return nil, err
	}
	defer cli.Close()

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{
		All: true,
	})
	if err != nil {
		return nil, err
	}

	var sessions []ContainerInfo
	for _, container := range containers {
		// Check if this is a worklet container
		for k, v := range container.Labels {
			if k == "worklet.session" && v == "true" {
				sessionID := container.Labels["worklet.session.id"]
				if sessionID == "" {
					// Use container name as fallback
					if len(container.Names) > 0 {
						sessionID = strings.TrimPrefix(container.Names[0], "/")
					}
				}
				sessions = append(sessions, ContainerInfo{
					ID:          sessionID,
					ContainerID: container.ID,
					Status:      container.State,
				})
				break
			}
		}
	}

	return sessions, nil
}

// GetContainerID returns the container ID for a given session ID
func GetContainerID(sessionID string) (string, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv)
	if err != nil {
		return "", err
	}
	defer cli.Close()

	containers, err := cli.ContainerList(context.Background(), container.ListOptions{
		All: true,
	})
	if err != nil {
		return "", err
	}

	for _, container := range containers {
		// Check by session ID label
		if container.Labels["worklet.session.id"] == sessionID {
			return container.ID, nil
		}
		// Check by container name
		for _, name := range container.Names {
			if strings.TrimPrefix(name, "/") == sessionID {
				return container.ID, nil
			}
		}
	}

	return "", fmt.Errorf("session %s not found", sessionID)
}
