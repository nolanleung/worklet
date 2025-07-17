package terminal

import (
	"context"
	"fmt"
	"strings"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type Fork struct {
	ID          string `json:"id"`
	ContainerID string `json:"container_id"`
	Status      string `json:"status"`
}

// ListForks returns a list of available worklet forks
func ListForks() ([]Fork, error) {
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

	var forks []Fork
	for _, container := range containers {
		// Check if this is a worklet container
		for k, v := range container.Labels {
			if k == "worklet.fork" && v == "true" {
				forkID := container.Labels["worklet.fork.id"]
				if forkID == "" {
					// Use container name as fallback
					if len(container.Names) > 0 {
						forkID = strings.TrimPrefix(container.Names[0], "/")
					}
				}
				forks = append(forks, Fork{
					ID:          forkID,
					ContainerID: container.ID,
					Status:      container.State,
				})
				break
			}
		}
	}

	return forks, nil
}

// GetContainerID returns the container ID for a given fork ID
func GetContainerID(forkID string) (string, error) {
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
		// Check by fork ID label
		if container.Labels["worklet.fork.id"] == forkID {
			return container.ID, nil
		}
		// Check by container name
		for _, name := range container.Names {
			if strings.TrimPrefix(name, "/") == forkID {
				return container.ID, nil
			}
		}
	}

	return "", fmt.Errorf("fork %s not found", forkID)
}
