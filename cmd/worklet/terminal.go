package worklet

import (
	"fmt"
	"os/exec"
	"runtime"
	"time"

	"github.com/nolanleung/worklet/pkg/terminal"
	"github.com/spf13/cobra"
)

var terminalCmd = &cobra.Command{
	Use:   "terminal",
	Short: "Start a web-based terminal server for container access",
	Long:  `Start a web-based terminal server that provides access to container shells through a browser interface.`,
	RunE:  runTerminal,
}

var (
	terminalPort     int
	openBrowser      bool
	terminalCORSOrigin string
)

func init() {
	terminalCmd.Flags().IntVarP(&terminalPort, "port", "p", 8080, "Port to run the terminal server on")
	terminalCmd.Flags().BoolVarP(&openBrowser, "open", "o", true, "Open browser automatically")
	terminalCmd.Flags().StringVar(&terminalCORSOrigin, "cors-origin", "*", "CORS allowed origin (use '*' to allow all origins)")
	rootCmd.AddCommand(terminalCmd)
}

func runTerminal(cmd *cobra.Command, args []string) error {
	server := terminal.NewServer(terminalPort)
	
	// Configure CORS
	server.SetCORSOrigin(terminalCORSOrigin)
	
	url := fmt.Sprintf("http://localhost:%d", terminalPort)
	fmt.Printf("Starting terminal server on %s\n", url)
	fmt.Printf("CORS origin: %s\n", terminalCORSOrigin)
	
	// Open browser if requested
	if openBrowser {
		go func() {
			// Small delay to ensure server is started
			time.Sleep(500 * time.Millisecond)
			openURL(url)
		}()
	}
	
	// Start server (blocks)
	return server.Start()
}

func openURL(url string) error {
	var cmd string
	var args []string

	switch runtime.GOOS {
	case "windows":
		cmd = "cmd"
		args = []string{"/c", "start", url}
	case "darwin":
		cmd = "open"
		args = []string{url}
	default: // "linux", "freebsd", "openbsd", "netbsd"
		cmd = "xdg-open"
		args = []string{url}
	}

	return exec.Command(cmd, args...).Start()
}