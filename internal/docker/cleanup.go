package docker

import (
	"context"
	"fmt"
	"os/exec"
	"strings"
)

// CleanupOptions configures cleanup behavior
type CleanupOptions struct {
	Force bool // Force removal of shared resources like pnpm volumes
}

// CleanupSession removes all Docker resources associated with a session
func CleanupSession(ctx context.Context, sessionID string, opts CleanupOptions) error {
	var errors []string
	
	// 1. Get session info before removal
	session, err := GetSessionInfo(ctx, sessionID)
	if err != nil {
		// Session might already be partially removed, continue with cleanup
		session = &SessionInfo{SessionID: sessionID}
	}
	
	// 2. Remove container (force removal)
	if session.ContainerID != "" {
		cmd := exec.CommandContext(ctx, "docker", "rm", "-f", session.ContainerID)
		if err := cmd.Run(); err != nil {
			errors = append(errors, fmt.Sprintf("container removal: %v", err))
		}
	}
	
	// 3. Remove session network
	networkName := GetSessionNetworkName(sessionID)
	if err := RemoveNetwork(networkName); err != nil {
		// Don't report error if network doesn't exist
		if !strings.Contains(err.Error(), "not found") {
			errors = append(errors, fmt.Sprintf("network removal: %v", err))
		}
	}
	
	// 4. Remove DinD volume
	dindVolume := fmt.Sprintf("worklet-%s", sessionID)
	if err := RemoveVolume(dindVolume); err != nil {
		// Don't report error if volume doesn't exist
		if !strings.Contains(err.Error(), "no such volume") {
			errors = append(errors, fmt.Sprintf("DinD volume removal: %v", err))
		}
	}
	
	// 5. Remove temporary image (if exists)
	if session.ProjectName != "" {
		imageName := fmt.Sprintf("worklet-temp-%s-%s", 
			strings.ToLower(session.ProjectName), sessionID)
		cmd := exec.CommandContext(ctx, "docker", "rmi", imageName)
		cmd.Run() // Ignore errors as image might not exist
	}
	
	// 6. Only remove pnpm volume if Force is true
	if opts.Force && session.ProjectName != "" {
		cleanupProjectVolumes(ctx, session.ProjectName, opts.Force)
	}
	
	if len(errors) > 0 {
		return fmt.Errorf("cleanup had errors: %s", strings.Join(errors, "; "))
	}
	
	return nil
}

// cleanupProjectVolumes removes project-specific volumes
func cleanupProjectVolumes(ctx context.Context, projectName string, force bool) {
	if !force {
		// Never remove pnpm volumes unless forced
		return
	}
	
	// Check if any other sessions exist for this project
	sessions, _ := ListSessionsByProject(ctx, projectName)
	if len(sessions) > 0 {
		return // Other sessions still using project volumes
	}
	
	// Remove pnpm store volume
	pnpmVolume := fmt.Sprintf("worklet-pnpm-store-%s", projectName)
	RemoveVolume(pnpmVolume) // Ignore errors
}

// CleanupAllOrphaned removes all orphaned Docker resources
func CleanupAllOrphaned(ctx context.Context, opts CleanupOptions) error {
	var cleaned []string
	
	// 1. Clean orphaned networks
	count, err := CleanupOrphanedNetworks()
	if err == nil && count > 0 {
		cleaned = append(cleaned, fmt.Sprintf("%d networks", count))
	}
	
	// 2. Clean orphaned volumes (respecting Force option)
	count, err = CleanupOrphanedVolumes(ctx, opts)
	if err == nil && count > 0 {
		cleaned = append(cleaned, fmt.Sprintf("%d volumes", count))
	}
	
	// 3. Clean temporary images
	count, err = CleanupOrphanedImages(ctx)
	if err == nil && count > 0 {
		cleaned = append(cleaned, fmt.Sprintf("%d images", count))
	}
	
	if len(cleaned) > 0 {
		fmt.Printf("Cleaned up: %s\n", strings.Join(cleaned, ", "))
	} else {
		fmt.Println("No orphaned resources found")
	}
	
	return nil
}

// CleanupOrphanedVolumes removes worklet volumes not associated with running containers
func CleanupOrphanedVolumes(ctx context.Context, opts CleanupOptions) (int, error) {
	cmd := exec.Command("docker", "volume", "ls", "--format", "{{.Name}}")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to list volumes: %w", err)
	}
	
	volumes := strings.Split(string(output), "\n")
	removedCount := 0
	
	// Get all active sessions
	sessions, _ := ListAllSessions(ctx)
	activeSessionIDs := make(map[string]bool)
	activeProjects := make(map[string]bool)
	for _, s := range sessions {
		activeSessionIDs[s.SessionID] = true
		if s.ProjectName != "" {
			activeProjects[s.ProjectName] = true
		}
	}
	
	for _, vol := range volumes {
		vol = strings.TrimSpace(vol)
		if vol == "" {
			continue
		}
		
		// Check session DinD volumes (worklet-sessionid)
		if strings.HasPrefix(vol, "worklet-") && 
		   !strings.Contains(vol, "pnpm-store") && 
		   !strings.Contains(vol, "credentials") {
			// Extract session ID (everything after "worklet-")
			sessionID := strings.TrimPrefix(vol, "worklet-")
			
			// Skip if it's the main network volume
			if sessionID == "network" {
				continue
			}
			
			// Remove if session doesn't exist
			if !activeSessionIDs[sessionID] {
				if err := RemoveVolume(vol); err == nil {
					removedCount++
					fmt.Printf("Removed orphaned volume: %s\n", vol)
				}
			}
		}
		
		// Only remove pnpm volumes if Force is enabled
		if opts.Force && strings.HasPrefix(vol, "worklet-pnpm-store-") {
			projectName := strings.TrimPrefix(vol, "worklet-pnpm-store-")
			if !activeProjects[projectName] {
				if err := RemoveVolume(vol); err == nil {
					removedCount++
					fmt.Printf("Removed orphaned pnpm volume: %s\n", vol)
				}
			}
		}
	}
	
	return removedCount, nil
}

// CleanupOrphanedImages removes temporary worklet images
func CleanupOrphanedImages(ctx context.Context) (int, error) {
	cmd := exec.Command("docker", "images", "--format", "{{.Repository}}:{{.Tag}}")
	output, err := cmd.Output()
	if err != nil {
		return 0, fmt.Errorf("failed to list images: %w", err)
	}
	
	images := strings.Split(string(output), "\n")
	removedCount := 0
	
	// Get all active sessions to check if images are still in use
	sessions, _ := ListAllSessions(ctx)
	activeImages := make(map[string]bool)
	for _, s := range sessions {
		if s.ProjectName != "" && s.SessionID != "" {
			imageName := fmt.Sprintf("worklet-temp-%s-%s", 
				strings.ToLower(s.ProjectName), s.SessionID)
			activeImages[imageName] = true
		}
	}
	
	for _, img := range images {
		img = strings.TrimSpace(img)
		if img == "" {
			continue
		}
		
		// Remove :latest or other tags for comparison
		imgName := strings.Split(img, ":")[0]
		
		if strings.HasPrefix(imgName, "worklet-temp-") && !activeImages[imgName] {
			cmd := exec.Command("docker", "rmi", img)
			if err := cmd.Run(); err == nil {
				removedCount++
				fmt.Printf("Removed orphaned image: %s\n", img)
			}
		}
	}
	
	return removedCount, nil
}