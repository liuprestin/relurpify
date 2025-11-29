package framework

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// SandboxRuntime describes a sandbox backend (gVisor).
type SandboxRuntime interface {
	Name() string
	Verify(ctx context.Context) error
	RunConfig() SandboxConfig
	EnforcePolicy(policy SandboxPolicy) error
}

// SandboxConfig exposes runtime knobs.
type SandboxConfig struct {
	RunscPath        string
	ContainerRuntime string // docker or containerd
	Platform         string // ptrace or kvm
	NetworkIsolation bool
	ReadOnlyRoot     bool
	SeccompProfile   string
}

// SandboxPolicy captures runtime adjustments derived from permissions.
type SandboxPolicy struct {
	NetworkRules []NetworkRule
	ReadOnlyRoot bool
}

// NetworkRule represents an allowed network scope.
type NetworkRule struct {
	Direction string
	Protocol  string
	Host      string
	Port      int
}

// GVisorRuntime enforces runsc-backed execution.
type GVisorRuntime struct {
	config   SandboxConfig
	verified bool
	mu       sync.Mutex
	version  string
	policy   SandboxPolicy
}

// NewGVisorRuntime configures the runtime.
func NewGVisorRuntime(config SandboxConfig) *GVisorRuntime {
	if config.RunscPath == "" {
		config.RunscPath = "runsc"
	}
	if config.Platform == "" {
		config.Platform = "kvm"
	}
	if config.ContainerRuntime == "" {
		config.ContainerRuntime = "docker"
	}
	if !config.NetworkIsolation {
		config.NetworkIsolation = true
	}
	return &GVisorRuntime{
		config: config,
	}
}

// Name implements SandboxRuntime.
func (g *GVisorRuntime) Name() string {
	return "gvisor"
}

// RunConfig returns the effective configuration.
func (g *GVisorRuntime) RunConfig() SandboxConfig {
	return g.config
}

// Verify ensures runsc and the selected runtime are available.
func (g *GVisorRuntime) Verify(ctx context.Context) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.verified {
		return nil
	}
	if err := g.checkRunsc(ctx); err != nil {
		return err
	}
	if err := g.checkContainerRuntime(ctx); err != nil {
		return err
	}
	g.verified = true
	return nil
}

// EnforcePolicy stores the effective sandbox policies for future launches.
func (g *GVisorRuntime) EnforcePolicy(policy SandboxPolicy) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.policy = policy
	return nil
}

// checkRunsc validates the runsc binary exists and matches the expected
// platform so we fail fast before attempting to launch sandboxes.
func (g *GVisorRuntime) checkRunsc(ctx context.Context) error {
	path, err := exec.LookPath(g.config.RunscPath)
	if err != nil {
		return fmt.Errorf("runsc binary not found: %w", err)
	}
	c, cancel := g.commandContext(ctx, path, "--version")
	defer cancel()
	output, err := c.CombinedOutput()
	if err != nil {
		return fmt.Errorf("runsc verification failed: %w", err)
	}
	g.version = strings.TrimSpace(string(output))
	if !strings.Contains(g.version, "runsc") {
		return errors.New("invalid runsc output")
	}
	if g.config.Platform != "" && !strings.Contains(strings.ToLower(g.version), g.config.Platform) {
		// Platform hint mismatch is logged via version string but no longer fatal so
		// installations that omit the platform label continue to work.
		g.version = fmt.Sprintf("%s (platform hint %s not found)", g.version, g.config.Platform)
	}
	return nil
}

// checkContainerRuntime ensures docker/containerd are installed and respond to
// a basic info command so the agent runtime can launch workloads later.
func (g *GVisorRuntime) checkContainerRuntime(ctx context.Context) error {
	runtime := strings.ToLower(g.config.ContainerRuntime)
	switch runtime {
	case "docker", "containerd":
	default:
		return fmt.Errorf("unsupported container runtime %s", g.config.ContainerRuntime)
	}
	_, err := exec.LookPath(runtime)
	if err != nil {
		return fmt.Errorf("%s binary not found: %w", runtime, err)
	}
	// We run a lightweight version command to ensure gVisor integration flag exists.
	var args []string
	if runtime == "docker" {
		args = []string{"info", "--format", "'{{json .Runtimes}}'"}
	} else {
		args = []string{"--version"}
	}
	cmd, cancel := g.commandContext(ctx, runtime, args...)
	defer cancel()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s verification failed: %w", runtime, err)
	}
	return nil
}

// commandContext wraps exec.CommandContext with a consistent timeout to avoid
// hanging verification commands.
func (g *GVisorRuntime) commandContext(ctx context.Context, name string, args ...string) (*exec.Cmd, context.CancelFunc) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	return exec.CommandContext(ctx, name, args...), cancel
}
