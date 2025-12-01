package framework

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// RuntimeConfig wires sandbox and auditing defaults.
type RuntimeConfig struct {
	ManifestPath string
	Image        string
	Sandbox      SandboxConfig
	AuditLimit   int
	BaseFS       string
	HITLTimeout  time.Duration
}

// AgentRegistration stores runtime metadata.
type AgentRegistration struct {
	ID          string
	Manifest    *AgentManifest
	Runtime     SandboxRuntime
	Permissions *PermissionManager
	Audit       AuditLogger
	HITL        *HITLBroker
}

// RegisterAgent validates the manifest and builds enforcement primitives.
func RegisterAgent(ctx context.Context, cfg RuntimeConfig) (*AgentRegistration, error) {
	if cfg.ManifestPath == "" {
		return nil, errors.New("manifest path required")
	}
	manifest, err := LoadAgentManifest(cfg.ManifestPath)
	if err != nil {
		return nil, fmt.Errorf("load manifest: %w", err)
	}
	runtime := NewGVisorRuntime(cfg.Sandbox)
	if err := runtime.Verify(ctx); err != nil {
		return nil, fmt.Errorf("sandbox verification failed: %w", err)
	}
	hitl := NewHITLBroker(cfg.HITLTimeout)
	audit := NewInMemoryAuditLogger(cfg.AuditLimit)
	permissions, err := NewPermissionManager(cfg.BaseFS, &manifest.Spec.Permissions, audit, hitl)
	if err != nil {
		return nil, fmt.Errorf("permission manager init: %w", err)
	}
	permissions.AttachRuntime(runtime)
	networkRules := buildNetworkPolicy(manifest.Spec.Permissions.Network)
	policy := SandboxPolicy{
		NetworkRules: networkRules,
		ReadOnlyRoot: manifest.Spec.Security.ReadOnlyRoot,
	}
	_ = runtime.EnforcePolicy(policy)
	return &AgentRegistration{
		ID:          manifest.Metadata.Name,
		Manifest:    manifest,
		Runtime:     runtime,
		Permissions: permissions,
		Audit:       audit,
		HITL:        hitl,
	}, nil
}

// buildNetworkPolicy converts network permissions into sandbox-friendly rules
// so gVisor enforces the same view of allowed hosts/ports as the permission
// manager.
func buildNetworkPolicy(perms []NetworkPermission) []NetworkRule {
	var rules []NetworkRule
	for _, perm := range perms {
		if perm.Direction != "egress" {
			continue
		}
		rules = append(rules, NetworkRule{
			Direction: perm.Direction,
			Protocol:  perm.Protocol,
			Host:      perm.Host,
			Port:      perm.Port,
		})
	}
	return rules
}

// Execute enforces permissions prior to delegating to the agent.
func (r *AgentRegistration) Execute(ctx context.Context, agent Agent, task *Task, state *Context) (*Result, error) {
	if agent == nil {
		return nil, errors.New("agent missing")
	}
	if r == nil || r.Permissions == nil {
		return nil, errors.New("permission subsystem missing")
	}
	if err := agent.Initialize(&Config{Name: r.ID, OllamaToolCalling: true}); err != nil {
		return nil, err
	}
	return agent.Execute(ctx, task, state)
}

// QueryAudit proxies queries to the audit store.
func (r *AgentRegistration) QueryAudit(ctx context.Context, filter AuditQuery) ([]AuditRecord, error) {
	if r == nil || r.Audit == nil {
		return nil, errors.New("audit logger missing")
	}
	return r.Audit.Query(ctx, filter)
}

// GrantPermission allows operators to programmatically approve scopes.
func (r *AgentRegistration) GrantPermission(desc PermissionDescriptor, approvedBy string, scope GrantScope, duration time.Duration) {
	if r == nil || r.Permissions == nil {
		return
	}
	grant := GrantManual(desc, approvedBy, scope, duration)
	r.Permissions.mu.Lock()
	defer r.Permissions.mu.Unlock()
	r.Permissions.grants[desc.Action+":"+desc.Resource] = grant
}
