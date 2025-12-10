package testsuite

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/lexcodex/relurpify/framework"
)

func TestToolRegistryPermissionEnforcement(t *testing.T) {
	base := t.TempDir()
	perms := framework.NewFileSystemPermissionSet(base, framework.FileSystemRead, framework.FileSystemList)
	perms.Network = []framework.NetworkPermission{{Direction: "egress", Protocol: "tcp", Host: "example.com", Port: 443}}
	manager, err := framework.NewPermissionManager(base, perms, nil, nil)
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}
	runtime := &recordingRuntime{}
	manager.AttachRuntime(runtime)

	allowedToolPerms := framework.NewFileSystemPermissionSet(base, framework.FileSystemRead)
	allowedToolPerms.Network = []framework.NetworkPermission{{Direction: "egress", Protocol: "tcp", Host: "example.com", Port: 443}}
	allowedTool := &permissionedTool{
		toolName: "workspace_reader",
		perms:    allowedToolPerms,
		manager:  manager,
		agent:    "agent-int",
		path:     filepath.Join(base, "file.txt"),
		host:     "example.com",
	}
	escapePerms := framework.NewFileSystemPermissionSet("/etc", framework.FileSystemRead)
	escapeTool := &permissionedTool{
		toolName: "escape",
		perms:    escapePerms,
		manager:  manager,
		agent:    "agent-int",
		path:     "/etc/passwd",
	}

	registry := framework.NewToolRegistry()
	if err := registry.Register(allowedTool); err != nil {
		t.Fatalf("register allowed tool: %v", err)
	}
	if err := registry.Register(escapeTool); err != nil {
		t.Fatalf("register escape tool: %v", err)
	}
	registry.UsePermissionManager("agent-int", manager)

	tool, _ := registry.Get("workspace_reader")
	state := framework.NewContext()
	if _, err := tool.Execute(context.Background(), state, nil); err != nil {
		t.Fatalf("expected allowed tool to run, got error: %v", err)
	}
	if value, _ := state.Get("tool:workspace_reader"); value != "ok" {
		t.Fatalf("tool state not recorded: %v", value)
	}
	if len(runtime.policies) == 0 || len(runtime.policies[len(runtime.policies)-1].NetworkRules) == 0 {
		t.Fatal("expected network policy to be enforced")
	}

	escape, _ := registry.Get("escape")
	if _, err := escape.Execute(context.Background(), framework.NewContext(), nil); err == nil {
		t.Fatal("expected permission error for escape tool")
	}
}

func TestToolRegistryNetworkHITLApproval(t *testing.T) {
	base := t.TempDir()
	hitl := &stubHITL{
		grants: []*framework.PermissionGrant{{
			ID: "grant-1",
			Permission: framework.PermissionDescriptor{
				Type:     framework.PermissionTypeNetwork,
				Action:   "net:egress",
				Resource: "api.service.local:443",
			},
			Scope: framework.GrantScopeSession,
		}},
	}
	perms := framework.NewFileSystemPermissionSet(base, framework.FileSystemRead)
	perms.Network = []framework.NetworkPermission{
		{Direction: "egress", Protocol: "tcp", Host: "api.service.local", Port: 443, HITLRequired: true},
	}
	manager, err := framework.NewPermissionManager(base, perms, nil, hitl)
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}

	toolPerms := framework.NewFileSystemPermissionSet(base, framework.FileSystemRead)
	toolPerms.Network = []framework.NetworkPermission{
		{Direction: "egress", Protocol: "tcp", Host: "api.service.local", Port: 443, HITLRequired: true},
	}
	netTool := &permissionedTool{
		toolName: "net_call",
		perms:    toolPerms,
		manager:  manager,
		agent:    "agent-net",
		host:     "api.service.local",
	}

	registry := framework.NewToolRegistry()
	if err := registry.Register(netTool); err != nil {
		t.Fatalf("register net tool: %v", err)
	}
	registry.UsePermissionManager("agent-net", manager)

	tool, _ := registry.Get("net_call")
	if _, err := tool.Execute(context.Background(), framework.NewContext(), nil); err != nil {
		t.Fatalf("expected HITL-enabled tool to run, got error: %v", err)
	}
	if len(hitl.requests) != 1 {
		t.Fatalf("expected exactly one HITL request, got %d", len(hitl.requests))
	}
	if _, err := tool.Execute(context.Background(), framework.NewContext(), nil); err != nil {
		t.Fatalf("expected cached grant to allow subsequent run: %v", err)
	}
	if len(hitl.requests) != 1 {
		t.Fatalf("expected cached grant to prevent duplicate HITL calls, got %d", len(hitl.requests))
	}
}

type recordingRuntime struct {
	policies []framework.SandboxPolicy
}

func (r *recordingRuntime) Name() string                       { return "recording" }
func (r *recordingRuntime) Verify(context.Context) error       { return nil }
func (r *recordingRuntime) RunConfig() framework.SandboxConfig { return framework.SandboxConfig{} }
func (r *recordingRuntime) EnforcePolicy(policy framework.SandboxPolicy) error {
	r.policies = append(r.policies, policy)
	return nil
}

type permissionedTool struct {
	toolName string
	perms    *framework.PermissionSet
	manager  *framework.PermissionManager
	agent    string
	path     string
	host     string
}

func (t *permissionedTool) Name() string        { return t.toolName }
func (t *permissionedTool) Description() string { return "integration test tool" }
func (t *permissionedTool) Category() string    { return "integration" }
func (t *permissionedTool) Parameters() []framework.ToolParameter {
	return nil
}
func (t *permissionedTool) Execute(ctx context.Context, state *framework.Context, args map[string]interface{}) (*framework.ToolResult, error) {
	if t.manager != nil {
		if t.path != "" {
			if err := t.manager.CheckFileAccess(ctx, t.agent, framework.FileSystemRead, t.path); err != nil {
				return nil, err
			}
		}
		if t.host != "" {
			if err := t.manager.CheckNetwork(ctx, t.agent, "egress", "tcp", t.host, 443); err != nil {
				return nil, err
			}
		}
	}
	state.Set("tool:"+t.toolName, "ok")
	return &framework.ToolResult{Success: true}, nil
}
func (t *permissionedTool) IsAvailable(context.Context, *framework.Context) bool { return true }
func (t *permissionedTool) Permissions() framework.ToolPermissions {
	return framework.ToolPermissions{Permissions: t.perms}
}

type stubHITL struct {
	grants   []*framework.PermissionGrant
	requests []framework.PermissionRequest
}

func (s *stubHITL) RequestPermission(ctx context.Context, req framework.PermissionRequest) (*framework.PermissionGrant, error) {
	s.requests = append(s.requests, req)
	if len(s.grants) == 0 {
		return &framework.PermissionGrant{Permission: req.Permission, Scope: framework.GrantScopeSession}, nil
	}
	grant := s.grants[0]
	s.grants = s.grants[1:]
	if grant.Permission.Action == "" {
		grant.Permission = req.Permission
	}
	return grant, nil
}
