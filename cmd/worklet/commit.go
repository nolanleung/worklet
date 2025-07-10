package worklet

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/nolanleung/worklet/internal/fork"
	"github.com/spf13/cobra"
)

var (
	commitMessage string
	commitAll     bool
	commitPush    bool
	commitBranch  string
)

var commitCmd = &cobra.Command{
	Use:   "commit [fork]",
	Short: "Commit changes in a worklet fork",
	Long: `Commit changes in a worklet fork with Git operations.
	
If run inside a fork, commits to the current fork.
Otherwise, specify a fork by index, ID, or name.`,
	Args: cobra.MaximumNArgs(1),
	RunE: runCommit,
}

func init() {
	commitCmd.Flags().StringVarP(&commitMessage, "message", "m", "", "Commit message")
	commitCmd.Flags().BoolVarP(&commitAll, "all", "a", false, "Stage all changes before committing")
	commitCmd.Flags().BoolVar(&commitPush, "push", false, "Push to remote after committing")
	commitCmd.Flags().StringVarP(&commitBranch, "branch", "b", "", "Create and switch to new branch before committing")
}

func runCommit(cmd *cobra.Command, args []string) error {
	// Determine which fork to use
	forkPath, err := determineForkPath(args)
	if err != nil {
		return err
	}

	// Change to fork directory
	originalDir, err := os.Getwd()
	if err != nil {
		return fmt.Errorf("failed to get current directory: %w", err)
	}
	
	if err := os.Chdir(forkPath); err != nil {
		return fmt.Errorf("failed to change to fork directory: %w", err)
	}
	defer os.Chdir(originalDir)

	// Check if it's a git repository
	if _, err := os.Stat(".git"); os.IsNotExist(err) {
		return fmt.Errorf("fork at %s is not a git repository", forkPath)
	}

	// Show git status
	fmt.Println("Current git status:")
	if err := runGitCommand("status", "--short"); err != nil {
		return fmt.Errorf("failed to get git status: %w", err)
	}

	// Check if there are changes to commit
	output, err := exec.Command("git", "status", "--porcelain").Output()
	if err != nil {
		return fmt.Errorf("failed to check for changes: %w", err)
	}
	
	if len(output) == 0 && commitBranch == "" {
		fmt.Println("No changes to commit.")
		return nil
	}

	// Create new branch if requested
	if commitBranch != "" {
		fmt.Printf("Creating and switching to branch: %s\n", commitBranch)
		if err := runGitCommand("checkout", "-b", commitBranch); err != nil {
			// Try switching if branch already exists
			if err := runGitCommand("checkout", commitBranch); err != nil {
				return fmt.Errorf("failed to create/switch branch: %w", err)
			}
		}
	}

	// Stage changes if requested
	if commitAll {
		fmt.Println("Staging all changes...")
		if err := runGitCommand("add", "-A"); err != nil {
			return fmt.Errorf("failed to stage changes: %w", err)
		}
	}

	// Check if there are staged changes
	stagedOutput, err := exec.Command("git", "diff", "--cached", "--name-only").Output()
	if err != nil {
		return fmt.Errorf("failed to check staged changes: %w", err)
	}
	
	if len(stagedOutput) == 0 {
		return fmt.Errorf("no changes staged for commit (use -a to stage all changes)")
	}

	// Get commit message if not provided
	if commitMessage == "" {
		commitMessage, err = getCommitMessage()
		if err != nil {
			return fmt.Errorf("failed to get commit message: %w", err)
		}
	}

	// Commit changes
	fmt.Printf("Committing with message: %s\n", commitMessage)
	if err := runGitCommand("commit", "-m", commitMessage); err != nil {
		return fmt.Errorf("failed to commit: %w", err)
	}

	// Push if requested
	if commitPush {
		// Get current branch name
		branchOutput, err := exec.Command("git", "rev-parse", "--abbrev-ref", "HEAD").Output()
		if err != nil {
			return fmt.Errorf("failed to get current branch: %w", err)
		}
		branch := strings.TrimSpace(string(branchOutput))
		
		fmt.Printf("Pushing to origin/%s...\n", branch)
		if err := runGitCommand("push", "origin", branch); err != nil {
			// Try to set upstream if push fails
			fmt.Println("Setting upstream and pushing...")
			if err := runGitCommand("push", "--set-upstream", "origin", branch); err != nil {
				return fmt.Errorf("failed to push: %w", err)
			}
		}
	}

	fmt.Printf("\nSuccessfully committed changes in fork: %s\n", forkPath)
	return nil
}

func determineForkPath(args []string) (string, error) {
	// Check if we're already in a fork
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current directory: %w", err)
	}

	// Look for .worklet.lock file
	if _, err := os.Stat(filepath.Join(cwd, ".worklet.lock")); err == nil {
		fmt.Printf("Using current fork: %s\n", cwd)
		return cwd, nil
	}

	// If no argument provided, list forks and ask user to select
	if len(args) == 0 {
		forks, err := fork.ListForks()
		if err != nil {
			return "", fmt.Errorf("failed to list forks: %w", err)
		}

		if len(forks) == 0 {
			return "", fmt.Errorf("no forks found")
		}

		// Display forks
		fmt.Println("Available forks:")
		for i, f := range forks {
			fmt.Printf("%d. %s (%s)\n", i+1, f.ID, f.Name)
		}

		// Get user selection
		fmt.Print("Select fork number: ")
		reader := bufio.NewReader(os.Stdin)
		input, err := reader.ReadString('\n')
		if err != nil {
			return "", fmt.Errorf("failed to read input: %w", err)
		}

		var selection int
		if _, err := fmt.Sscanf(strings.TrimSpace(input), "%d", &selection); err != nil {
			return "", fmt.Errorf("invalid selection")
		}

		if selection < 1 || selection > len(forks) {
			return "", fmt.Errorf("selection out of range")
		}

		return forks[selection-1].Path, nil
	}

	// Use the provided fork identifier
	return fork.ResolveFork(args[0])
}

func runGitCommand(args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func getCommitMessage() (string, error) {
	fmt.Print("Enter commit message: ")
	reader := bufio.NewReader(os.Stdin)
	message, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	
	message = strings.TrimSpace(message)
	if message == "" {
		return "", fmt.Errorf("commit message cannot be empty")
	}
	
	return message, nil
}