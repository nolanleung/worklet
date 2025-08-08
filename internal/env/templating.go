package env

import (
	"fmt"
	"regexp"
	"strings"
)

// TemplateContext contains the context for template processing
type TemplateContext struct {
	SessionID   string
	ProjectName string
	Services    []ServiceInfo
}

// ServiceInfo contains service information for templating
type ServiceInfo struct {
	Name      string
	Port      int
	Subdomain string
}

// templatePattern matches {{ services.<name>.<property> }} syntax
var templatePattern = regexp.MustCompile(`\{\{\s*services\.(\w+)\.(url|host|port)\s*\}\}`)

// sessionPattern matches {{ session.<property> }} syntax
var sessionPattern = regexp.MustCompile(`\{\{\s*session\.(id)\s*\}\}`)

// projectPattern matches {{ project.<property> }} syntax
var projectPattern = regexp.MustCompile(`\{\{\s*project\.(name)\s*\}\}`)

// ProcessTemplate processes environment file content and replaces template variables
func ProcessTemplate(content string, ctx TemplateContext) string {
	// Build service map for quick lookup
	serviceMap := make(map[string]ServiceInfo)
	for _, svc := range ctx.Services {
		serviceMap[svc.Name] = svc
	}

	// Replace service references
	result := templatePattern.ReplaceAllStringFunc(content, func(match string) string {
		matches := templatePattern.FindStringSubmatch(match)
		if len(matches) != 3 {
			return match // Return original if no match
		}

		serviceName := matches[1]
		property := matches[2]

		service, ok := serviceMap[serviceName]
		if !ok {
			// Service not found, return original
			return match
		}

		// Generate the appropriate value based on property
		switch property {
		case "url":
			subdomain := service.Subdomain
			if subdomain == "" {
				subdomain = service.Name
			}
			return fmt.Sprintf("http://%s.%s-%s.local.worklet.sh", 
				subdomain, ctx.ProjectName, ctx.SessionID)
		case "host":
			subdomain := service.Subdomain
			if subdomain == "" {
				subdomain = service.Name
			}
			return fmt.Sprintf("%s.%s-%s.local.worklet.sh", 
				subdomain, ctx.ProjectName, ctx.SessionID)
		case "port":
			return fmt.Sprintf("%d", service.Port)
		default:
			return match
		}
	})

	// Replace session references
	result = sessionPattern.ReplaceAllStringFunc(result, func(match string) string {
		matches := sessionPattern.FindStringSubmatch(match)
		if len(matches) != 2 {
			return match
		}

		property := matches[1]
		switch property {
		case "id":
			return ctx.SessionID
		default:
			return match
		}
	})

	// Replace project references
	result = projectPattern.ReplaceAllStringFunc(result, func(match string) string {
		matches := projectPattern.FindStringSubmatch(match)
		if len(matches) != 2 {
			return match
		}

		property := matches[1]
		switch property {
		case "name":
			return ctx.ProjectName
		default:
			return match
		}
	})

	return result
}

// GetServiceEnvironmentVariables generates environment variables for all services
func GetServiceEnvironmentVariables(ctx TemplateContext) map[string]string {
	envVars := make(map[string]string)

	for _, service := range ctx.Services {
		subdomain := service.Subdomain
		if subdomain == "" {
			subdomain = service.Name
		}

		// Generate URL
		url := fmt.Sprintf("http://%s.%s-%s.local.worklet.sh", 
			subdomain, ctx.ProjectName, ctx.SessionID)
		
		// Generate host
		host := fmt.Sprintf("%s.%s-%s.local.worklet.sh", 
			subdomain, ctx.ProjectName, ctx.SessionID)

		// Create standard environment variables for each service
		serviceNameUpper := strings.ToUpper(service.Name)
		envVars[fmt.Sprintf("WORKLET_SERVICE_%s_URL", serviceNameUpper)] = url
		envVars[fmt.Sprintf("WORKLET_SERVICE_%s_HOST", serviceNameUpper)] = host
		envVars[fmt.Sprintf("WORKLET_SERVICE_%s_PORT", serviceNameUpper)] = fmt.Sprintf("%d", service.Port)
	}

	// Add session and project info
	envVars["WORKLET_SESSION_ID"] = ctx.SessionID
	envVars["WORKLET_PROJECT_NAME"] = ctx.ProjectName

	return envVars
}