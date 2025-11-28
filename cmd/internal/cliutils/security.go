package cliutils

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// BootstrapRuntime wires the sandbox/permission stack into a registry.
func BootstrapRuntime(ctx context.Context, workspace, manifestPath string, registry *framework.ToolRegistry) (*framework.AgentRegistration, error) {
	if registry == nil {
		return nil, errors.New("tool registry required")
	}
	if workspace == "" {
		workspace = "."
	}
	absWorkspace, err := filepath.Abs(workspace)
	if err != nil {
		return nil, err
	}
	if manifestPath == "" {
		manifestPath = os.Getenv("RELURPIFY_AGENT_MANIFEST")
	}
	if manifestPath == "" {
		manifestPath = filepath.Join(absWorkspace, "agent.manifest.yaml")
	}
	cfg := framework.RuntimeConfig{
		ManifestPath: manifestPath,
		BaseFS:       absWorkspace,
		AuditLimit:   4096,
		Sandbox: framework.SandboxConfig{
			RunscPath:        envOrDefault("RELURPIFY_RUNSC", "runsc"),
			ContainerRuntime: envOrDefault("RELURPIFY_CONTAINER_RUNTIME", "docker"),
			Platform:         envOrDefault("RELURPIFY_GVISOR_PLATFORM", "kvm"),
			NetworkIsolation: true,
			ReadOnlyRoot:     true,
			SeccompProfile:   "default",
		},
		HITLTimeout: 2 * time.Minute,
	}
	registration, err := framework.RegisterAgent(ctx, cfg)
	if err != nil {
		return nil, err
	}
	registry.UsePermissionManager(registration.Manifest.Metadata.Name, registration.Permissions)
	return registration, nil
}

func envOrDefault(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
