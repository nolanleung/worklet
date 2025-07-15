package worklet

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/nolanleung/worklet/internal/config"
	"github.com/nolanleung/worklet/pkg/proxy"
	"github.com/spf13/cobra"
)

var proxyCmd = &cobra.Command{
	Use:   "proxy",
	Short: "Manage the nginx reverse proxy for fork services",
	Long:  `Start, stop, or check the status of the nginx reverse proxy that routes traffic to fork services.`,
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		if proxyDebug {
			proxy.EnableDebug()
			proxy.Debug("Debug mode enabled")
		}
	},
}

var proxyStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the nginx reverse proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		proxy.Debug("Starting proxy initialization")
		
		// Initialize global proxy
		if err := proxy.InitGlobalProxy(); err != nil {
			proxy.DebugError("proxy initialization", err)
			return fmt.Errorf("failed to initialize proxy: %w", err)
		}
		proxy.Debug("Proxy initialized successfully")

		// Start the proxy
		ctx := context.Background()
		proxy.Debug("Starting proxy server")
		if err := proxy.StartGlobalProxy(ctx); err != nil {
			proxy.DebugError("proxy start", err)
			return fmt.Errorf("failed to start proxy: %w", err)
		}

		fmt.Println("Proxy started successfully")
		fmt.Println("Services will be available at: http://{service}.{random}.fork.local.worklet.sh")
		return nil
	},
}

var proxyStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the nginx reverse proxy",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize global proxy if needed
		if err := proxy.InitGlobalProxy(); err != nil {
			return fmt.Errorf("failed to initialize proxy: %w", err)
		}

		// Stop the proxy
		if err := proxy.StopGlobalProxy(); err != nil {
			return fmt.Errorf("failed to stop proxy: %w", err)
		}

		fmt.Println("Proxy stopped")
		return nil
	},
}

var proxyStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show proxy status and active mappings",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize global proxy if needed
		if err := proxy.InitGlobalProxy(); err != nil {
			return fmt.Errorf("failed to initialize proxy: %w", err)
		}

		// Get proxy server
		server, err := proxy.GetGlobalProxy()
		if err != nil {
			return err
		}

		// Show status
		if server.IsRunning() {
			fmt.Println("Proxy Status: Running")
			
			// Check health
			if err := server.CheckHealth(); err != nil {
				fmt.Printf("Health Check: FAILED - %v\n", err)
			} else {
				fmt.Println("Health Check: OK")
			}
		} else {
			fmt.Println("Proxy Status: Stopped")
			fmt.Println("\nTo view logs from the last run, use: worklet proxy logs")
		}

		// Show mappings
		mappings := server.GetManager().GetAllMappings()
		if len(mappings) == 0 {
			fmt.Println("\nNo active fork mappings")
		} else {
			fmt.Println("\nActive Fork Mappings:")
			for forkID, mapping := range mappings {
				fmt.Printf("\nFork: %s\n", forkID)
				for serviceName, service := range mapping.ServicePorts {
					url, _ := mapping.GetServiceURL(serviceName)
					fmt.Printf("  %s: %s (container: %s, port: %d)\n", 
						serviceName, url, service.ContainerName, service.ContainerPort)
				}
			}
		}

		return nil
	},
}

var proxyLogsCmd = &cobra.Command{
	Use:   "logs",
	Short: "Show nginx proxy container logs",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize global proxy if needed
		if err := proxy.InitGlobalProxy(); err != nil {
			return fmt.Errorf("failed to initialize proxy: %w", err)
		}

		// Get proxy server
		server, err := proxy.GetGlobalProxy()
		if err != nil {
			return err
		}

		// Get logs
		logs, err := server.GetLogs(100) // Last 100 lines
		if err != nil {
			return fmt.Errorf("failed to get logs: %w", err)
		}

		fmt.Println(logs)
		return nil
	},
}

var proxyInspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Show detailed nginx proxy container information",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize global proxy if needed
		if err := proxy.InitGlobalProxy(); err != nil {
			return fmt.Errorf("failed to initialize proxy: %w", err)
		}

		// Get proxy server
		server, err := proxy.GetGlobalProxy()
		if err != nil {
			return err
		}

		// Get container info
		info, err := server.GetContainerInfo()
		if err != nil {
			return fmt.Errorf("failed to inspect container: %w", err)
		}

		fmt.Println(info)
		return nil
	},
}

var (
	proxyRegisterServices []string
	proxyRegisterFromConfig bool
	proxyListJSON bool
	proxyListVerbose bool
	proxyDebug bool
)

var proxyRegisterCmd = &cobra.Command{
	Use:   "register <fork-id>",
	Short: "Manually register a fork with the proxy",
	Long: `Register a fork with the proxy to expose services via unique URLs.
	
Examples:
  # Register with explicit services
  worklet proxy register my-fork --service api:3000:api --service web:8080:app
  
  # Register using config from current directory
  worklet proxy register my-fork --from-config`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		forkID := args[0]
		
		// Initialize global proxy
		if err := proxy.InitGlobalProxy(); err != nil {
			return fmt.Errorf("failed to initialize proxy: %w", err)
		}
		
		var services []proxy.ServicePort
		
		if proxyRegisterFromConfig {
			// Load services from config
			cfg, err := config.LoadConfig(".")
			if err != nil {
				return fmt.Errorf("failed to load config: %w", err)
			}
			
			if len(cfg.Services) == 0 {
				return fmt.Errorf("no services defined in .worklet.jsonc")
			}
			
			// Convert config services to proxy services
			for _, svc := range cfg.Services {
				services = append(services, proxy.ServicePort{
					ServiceName:   svc.Name,
					ContainerPort: svc.Port,
					Subdomain:     svc.Subdomain,
				})
			}
		} else {
			// Parse services from command line
			if len(proxyRegisterServices) == 0 {
				return fmt.Errorf("no services specified. Use --service flags or --from-config")
			}
			
			for _, svcStr := range proxyRegisterServices {
				parts := strings.Split(svcStr, ":")
				if len(parts) != 3 {
					return fmt.Errorf("invalid service format: %s (expected name:port:subdomain)", svcStr)
				}
				
				port := 0
				if _, err := fmt.Sscanf(parts[1], "%d", &port); err != nil {
					return fmt.Errorf("invalid port in service %s: %w", svcStr, err)
				}
				
				services = append(services, proxy.ServicePort{
					ServiceName:   parts[0],
					ContainerPort: port,
					Subdomain:     parts[2],
				})
			}
		}
		
		// Register with proxy
		mapping, err := proxy.RegisterForkWithProxy(forkID, services)
		if err != nil {
			return fmt.Errorf("failed to register fork: %w", err)
		}
		
		// Display URLs
		fmt.Printf("Fork '%s' registered successfully!\n\n", forkID)
		fmt.Println("Proxy URLs:")
		for _, svc := range services {
			url, _ := mapping.GetServiceURL(svc.ServiceName)
			fmt.Printf("  %s: %s (port %d)\n", svc.ServiceName, url, svc.ContainerPort)
		}
		fmt.Println("\nNote: Ensure your services are running on the specified ports.")
		
		return nil
	},
}

var proxyUnregisterCmd = &cobra.Command{
	Use:   "unregister <fork-id>",
	Short: "Remove a fork from the proxy",
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		forkID := args[0]
		
		// Initialize global proxy
		if err := proxy.InitGlobalProxy(); err != nil {
			return fmt.Errorf("failed to initialize proxy: %w", err)
		}
		
		// Unregister fork
		if err := proxy.UnregisterForkFromProxy(forkID); err != nil {
			return fmt.Errorf("failed to unregister fork: %w", err)
		}
		
		fmt.Printf("Fork '%s' unregistered successfully\n", forkID)
		return nil
	},
}

var proxyListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all registered forks and their services",
	RunE: func(cmd *cobra.Command, args []string) error {
		// Initialize global proxy if needed
		if err := proxy.InitGlobalProxy(); err != nil {
			return fmt.Errorf("failed to initialize proxy: %w", err)
		}

		// Get proxy server
		server, err := proxy.GetGlobalProxy()
		if err != nil {
			return err
		}

		// Get all mappings
		mappings := server.GetManager().GetAllMappings()
		
		if proxyListJSON {
			// JSON output
			type ServiceJSON struct {
				Name      string `json:"name"`
				Port      int    `json:"port"`
				Subdomain string `json:"subdomain"`
				URL       string `json:"url"`
				Container string `json:"container,omitempty"`
			}
			
			type ForkJSON struct {
				ID       string        `json:"id"`
				Host     string        `json:"host"`
				Services []ServiceJSON `json:"services"`
			}
			
			type OutputJSON struct {
				Forks         []ForkJSON `json:"forks"`
				TotalForks    int        `json:"total_forks"`
				TotalServices int        `json:"total_services"`
			}
			
			output := OutputJSON{
				Forks:      []ForkJSON{},
				TotalForks: len(mappings),
			}
			
			for forkID, mapping := range mappings {
				fork := ForkJSON{
					ID:       forkID,
					Host:     mapping.RandomHost,
					Services: []ServiceJSON{},
				}
				
				for _, service := range mapping.ServicePorts {
					url, _ := mapping.GetServiceURL(service.ServiceName)
					svc := ServiceJSON{
						Name:      service.ServiceName,
						Port:      service.ContainerPort,
						Subdomain: service.Subdomain,
						URL:       url,
					}
					if proxyListVerbose {
						svc.Container = service.ContainerName
					}
					fork.Services = append(fork.Services, svc)
					output.TotalServices++
				}
				
				output.Forks = append(output.Forks, fork)
			}
			
			jsonOutput, err := json.MarshalIndent(output, "", "  ")
			if err != nil {
				return fmt.Errorf("failed to marshal JSON: %w", err)
			}
			fmt.Println(string(jsonOutput))
			
		} else {
			// Table output
			if len(mappings) == 0 {
				fmt.Println("No forks registered with the proxy")
				return nil
			}
			
			// Calculate column widths
			maxForkLen := 15
			maxServiceLen := 15
			for forkID, mapping := range mappings {
				if len(forkID) > maxForkLen {
					maxForkLen = len(forkID)
				}
				for serviceName := range mapping.ServicePorts {
					if len(serviceName) > maxServiceLen {
						maxServiceLen = len(serviceName)
					}
				}
			}
			
			// Print header
			fmt.Printf("%-*s %-*s %s\n", maxForkLen, "FORK ID", maxServiceLen, "SERVICE", "URL")
			fmt.Printf("%s %s %s\n", 
				strings.Repeat("-", maxForkLen),
				strings.Repeat("-", maxServiceLen),
				strings.Repeat("-", 50))
			
			// Print mappings
			totalServices := 0
			for forkID, mapping := range mappings {
				first := true
				for serviceName, service := range mapping.ServicePorts {
					url, _ := mapping.GetServiceURL(serviceName)
					if first {
						fmt.Printf("%-*s %-*s %s\n", maxForkLen, forkID, maxServiceLen, serviceName, url)
						first = false
					} else {
						fmt.Printf("%-*s %-*s %s\n", maxForkLen, "", maxServiceLen, serviceName, url)
					}
					
					if proxyListVerbose {
						fmt.Printf("%-*s %-*s   Container: %s, Port: %d\n", 
							maxForkLen, "", maxServiceLen, "", 
							service.ContainerName, service.ContainerPort)
					}
					totalServices++
				}
			}
			
			// Print summary
			fmt.Printf("\nTotal: %d fork%s, %d service%s\n", 
				len(mappings), 
				pluralize(len(mappings)),
				totalServices,
				pluralize(totalServices))
		}
		
		return nil
	},
}

func pluralize(count int) string {
	if count == 1 {
		return ""
	}
	return "s"
}

func init() {
	// Add persistent debug flag to all proxy commands
	proxyCmd.PersistentFlags().BoolVar(&proxyDebug, "debug", false, "Enable debug logging")
	
	proxyRegisterCmd.Flags().StringSliceVar(&proxyRegisterServices, "service", []string{}, "Service definition in format name:port:subdomain (can be used multiple times)")
	proxyRegisterCmd.Flags().BoolVar(&proxyRegisterFromConfig, "from-config", false, "Load services from .worklet.jsonc in current directory")
	
	proxyListCmd.Flags().BoolVar(&proxyListJSON, "json", false, "Output in JSON format")
	proxyListCmd.Flags().BoolVar(&proxyListVerbose, "verbose", false, "Show additional details")
	
	proxyCmd.AddCommand(proxyStartCmd)
	proxyCmd.AddCommand(proxyStopCmd)
	proxyCmd.AddCommand(proxyStatusCmd)
	proxyCmd.AddCommand(proxyLogsCmd)
	proxyCmd.AddCommand(proxyInspectCmd)
	proxyCmd.AddCommand(proxyRegisterCmd)
	proxyCmd.AddCommand(proxyUnregisterCmd)
	proxyCmd.AddCommand(proxyListCmd)
}