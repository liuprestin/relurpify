package framework

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// PermissionType enumerates the supported permission families.
type PermissionType string

const (
	PermissionTypeFilesystem PermissionType = "filesystem"
	PermissionTypeExecutable PermissionType = "executable"
	PermissionTypeNetwork    PermissionType = "network"
	PermissionTypeCapability PermissionType = "capability"
	PermissionTypeIPC        PermissionType = "ipc"
	PermissionTypeHITL       PermissionType = "hitl"
	permissionMatchAll                      = "**"
)

// FileSystemAction enumerates filesystem operations.
type FileSystemAction string

const (
	FileSystemRead    FileSystemAction = "fs:read"
	FileSystemWrite   FileSystemAction = "fs:write"
	FileSystemExecute FileSystemAction = "fs:execute"
	FileSystemList    FileSystemAction = "fs:list"
)

// FileSystemPermission scopes access to a portion of the workspace.
type FileSystemPermission struct {
	Action        FileSystemAction `json:"action" yaml:"action"`
	Path          string           `json:"path" yaml:"path"`
	Justification string           `json:"justification,omitempty" yaml:"justification,omitempty"`
	HITLRequired  bool             `json:"hitl_required,omitempty" yaml:"hitl_required,omitempty"`
	ReadOnlyMount bool             `json:"read_only_mount,omitempty" yaml:"read_only_mount,omitempty"`
}

// ExecutablePermission restricts binary execution.
type ExecutablePermission struct {
	Binary        string   `json:"binary" yaml:"binary"`
	Args          []string `json:"args,omitempty" yaml:"args,omitempty"`
	Env           []string `json:"env,omitempty" yaml:"env,omitempty"`
	Checksum      string   `json:"checksum,omitempty" yaml:"checksum,omitempty"`
	HITLRequired  bool     `json:"hitl_required,omitempty" yaml:"hitl_required,omitempty"`
	ProxyRequired bool     `json:"proxy_required,omitempty" yaml:"proxy_required,omitempty"`
}

// NetworkPermission describes network access.
type NetworkPermission struct {
	Direction    string `json:"direction" yaml:"direction"` // egress or ingress
	Protocol     string `json:"protocol" yaml:"protocol"`
	Host         string `json:"host,omitempty" yaml:"host,omitempty"`
	Port         int    `json:"port,omitempty" yaml:"port,omitempty"`
	Description  string `json:"description,omitempty" yaml:"description,omitempty"`
	HITLRequired bool   `json:"hitl_required,omitempty" yaml:"hitl_required,omitempty"`
}

// CapabilityPermission enumerates Linux capability requirements.
type CapabilityPermission struct {
	Capability    string `json:"capability" yaml:"capability"`
	Justification string `json:"justification,omitempty" yaml:"justification,omitempty"`
}

// IPCPermission restricts inter-process communication.
type IPCPermission struct {
	Kind         string `json:"kind" yaml:"kind"` // pipe/socket/signal
	Target       string `json:"target" yaml:"target"`
	Description  string `json:"description,omitempty" yaml:"description,omitempty"`
	HITLRequired bool   `json:"hitl_required,omitempty" yaml:"hitl_required,omitempty"`
}

// PermissionSet aggregates the permissions declared by an agent manifest.
type PermissionSet struct {
	FileSystem   []FileSystemPermission `json:"filesystem,omitempty" yaml:"filesystem,omitempty"`
	Executables  []ExecutablePermission `json:"executables,omitempty" yaml:"executables,omitempty"`
	Network      []NetworkPermission    `json:"network,omitempty" yaml:"network,omitempty"`
	Capabilities []CapabilityPermission `json:"capabilities,omitempty" yaml:"capabilities,omitempty"`
	IPC          []IPCPermission        `json:"ipc,omitempty" yaml:"ipc,omitempty"`
	HITLRequired []string               `json:"hitl_required,omitempty" yaml:"hitl_required,omitempty"`
}

// Validate ensures the permission declaration is consistent.
func (p *PermissionSet) Validate() error {
	if p == nil {
		return errors.New("permission set missing")
	}
	if len(p.FileSystem) == 0 && len(p.Executables) == 0 {
		return errors.New("permission set must declare at least filesystem or executable scopes")
	}
	for _, perm := range p.FileSystem {
		if perm.Path == "" {
			return fmt.Errorf("filesystem permission %s missing path", perm.Action)
		}
		if !strings.HasPrefix(string(perm.Action), "fs:") {
			return fmt.Errorf("invalid filesystem action %s", perm.Action)
		}
		if err := validateGlobPath(perm.Path); err != nil {
			return fmt.Errorf("invalid filesystem path %s: %w", perm.Path, err)
		}
	}
	for _, exec := range p.Executables {
		if exec.Binary == "" {
			return errors.New("executable permission missing binary")
		}
		if strings.Contains(exec.Binary, "/") {
			return fmt.Errorf("executable %s must be referenced by name", exec.Binary)
		}
	}
	for _, net := range p.Network {
		if net.Direction == "" {
			return errors.New("network permission missing direction")
		}
		if net.Protocol == "" {
			return fmt.Errorf("network permission for %s missing protocol", net.Direction)
		}
		if net.Direction == "egress" && net.Host == "" {
			return errors.New("egress network permission must declare host")
		}
	}
	for _, cap := range p.Capabilities {
		if cap.Capability == "" {
			return errors.New("capability permission missing capability")
		}
	}
	for _, ipc := range p.IPC {
		if ipc.Kind == "" || ipc.Target == "" {
			return errors.New("ipc permission missing kind or target")
		}
	}
	return nil
}

// PermissionDescriptor describes a single permission decision.
type PermissionDescriptor struct {
	Type         PermissionType
	Action       string
	Resource     string
	Metadata     map[string]string
	RequiresHITL bool
}

// PermissionDeniedError wraps denials with structured context.
type PermissionDeniedError struct {
	Descriptor PermissionDescriptor
	Message    string
}

// Error implements the error interface so permission denials bubble up with a
// consistent message format.
func (e *PermissionDeniedError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("permission denied: %s (%s)", e.Descriptor.Action, e.Message)
}

// PermissionManager enforces the declared permission set for runtime actions.
type PermissionManager struct {
	basePath   string
	declared   *PermissionSet
	audit      AuditLogger
	hitl       HITLProvider
	runtime    SandboxRuntime
	grants     map[string]*PermissionGrant
	mu         sync.RWMutex
	grantClock func() time.Time
	netPolicy  []NetworkRule
}

// NewPermissionManager creates an enforcement instance.
func NewPermissionManager(basePath string, declared *PermissionSet, audit AuditLogger, hitl HITLProvider) (*PermissionManager, error) {
	if declared == nil {
		return nil, errors.New("permission manager requires permission set")
	}
	if err := declared.Validate(); err != nil {
		return nil, err
	}
	pm := &PermissionManager{
		basePath:   basePath,
		declared:   declared,
		audit:      audit,
		hitl:       hitl,
		grants:     make(map[string]*PermissionGrant),
		grantClock: time.Now,
	}
	pm.inflateScopes()
	return pm, nil
}

// AttachRuntime allows the manager to push policy updates to the sandbox.
func (m *PermissionManager) AttachRuntime(runtime SandboxRuntime) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.runtime = runtime
	if len(m.netPolicy) > 0 {
		_ = runtime.EnforcePolicy(SandboxPolicy{NetworkRules: m.netPolicy})
	}
}

// inflateScopes rewrites any workspace placeholders inside the declared
// filesystem permissions so later matching can operate on concrete paths.
func (m *PermissionManager) inflateScopes() {
	if m == nil || m.declared == nil {
		return
	}
	ws := filepath.ToSlash(filepath.Clean(m.basePath))
	for i := range m.declared.FileSystem {
		m.declared.FileSystem[i].Path = expandWorkspacePlaceholder(ws, m.declared.FileSystem[i].Path)
	}
}

// expandWorkspacePlaceholder replaces instances of ${workspace} markers with
// the actual base path, keeping relative globs compatible with matchers.
func expandWorkspacePlaceholder(workspace, pattern string) string {
	if pattern == "" {
		return pattern
	}
	replacer := strings.NewReplacer(
		"${workspace}", workspace,
		"${WORKSPACE}", workspace,
		"{{workspace}}", workspace,
		"{{WORKSPACE}}", workspace,
	)
	resolved := filepath.ToSlash(replacer.Replace(pattern))
	if filepath.IsAbs(resolved) {
		return resolved
	}
	if workspace == "" {
		return filepath.ToSlash(resolved)
	}
	if strings.HasPrefix(resolved, "./") {
		resolved = strings.TrimPrefix(resolved, "./")
	}
	return filepath.ToSlash(filepath.Join(workspace, resolved))
}

// AuthorizeTool ensures the tool requirements fit the declared permissions.
func (m *PermissionManager) AuthorizeTool(ctx context.Context, agentID string, tool Tool, args map[string]interface{}) error {
	if m == nil || tool == nil {
		return errors.New("permission manager or tool missing")
	}
	requirements := tool.Permissions()
	if err := requirements.Validate(); err != nil {
		return fmt.Errorf("tool %s permission invalid: %w", tool.Name(), err)
	}
	if err := m.ensureSubset(requirements.Permissions); err != nil {
		return fmt.Errorf("tool %s exceeds agent permissions: %w", tool.Name(), err)
	}
	desc := PermissionDescriptor{
		Type:     PermissionTypeHITL,
		Action:   fmt.Sprintf("tool:%s", tool.Name()),
		Resource: agentID,
	}
	m.log(ctx, agentID, desc, "tool_allowed", nil)
	return nil
}

// CheckFileAccess validates filesystem access.
func (m *PermissionManager) CheckFileAccess(ctx context.Context, agentID string, action FileSystemAction, path string) error {
	if m == nil {
		return errors.New("permission manager missing")
	}
	clean, err := m.normalizePath(path)
	if err != nil {
		return err
	}
	perm := m.findFilesystemPermission(action, clean)
	if perm == nil {
		return m.deny(ctx, agentID, PermissionDescriptor{
			Type:     PermissionTypeFilesystem,
			Action:   string(action),
			Resource: clean,
		}, "not declared")
	}
	if perm.HITLRequired {
		if err := m.ensureGrant(ctx, agentID, PermissionDescriptor{
			Type:         PermissionTypeFilesystem,
			Action:       string(action),
			Resource:     perm.Path,
			RequiresHITL: true,
		}); err != nil {
			return err
		}
	}
	m.log(ctx, agentID, PermissionDescriptor{
		Type:     PermissionTypeFilesystem,
		Action:   string(action),
		Resource: clean,
	}, "granted", map[string]interface{}{
		"pattern": perm.Path,
	})
	return nil
}

// CheckExecutable validates binary execution.
func (m *PermissionManager) CheckExecutable(ctx context.Context, agentID, binary string, args []string, env []string) error {
	perm := m.findExecutablePermission(binary)
	if perm == nil {
		return m.deny(ctx, agentID, PermissionDescriptor{
			Type:     PermissionTypeExecutable,
			Action:   fmt.Sprintf("exec:binary:%s", binary),
			Resource: binary,
		}, "binary not declared")
	}
	if len(perm.Args) > 0 && !matchArgs(perm.Args, args) {
		return m.deny(ctx, agentID, PermissionDescriptor{
			Type:     PermissionTypeExecutable,
			Action:   fmt.Sprintf("exec:args:%s", strings.Join(args, " ")),
			Resource: binary,
		}, "arguments rejected")
	}
	if len(perm.Env) > 0 && !matchEnv(perm.Env, env) {
		return m.deny(ctx, agentID, PermissionDescriptor{
			Type:     PermissionTypeExecutable,
			Action:   "exec:env",
			Resource: binary,
		}, "environment rejected")
	}
	if perm.HITLRequired {
		if err := m.ensureGrant(ctx, agentID, PermissionDescriptor{
			Type:         PermissionTypeExecutable,
			Action:       fmt.Sprintf("exec:binary:%s", binary),
			Resource:     binary,
			RequiresHITL: true,
		}); err != nil {
			return err
		}
	}
	m.log(ctx, agentID, PermissionDescriptor{
		Type:     PermissionTypeExecutable,
		Action:   fmt.Sprintf("exec:%s", binary),
		Resource: binary,
	}, "granted", map[string]interface{}{
		"args": args,
		"env":  env,
	})
	return nil
}

// CheckNetwork validates network access.
func (m *PermissionManager) CheckNetwork(ctx context.Context, agentID string, direction string, protocol string, host string, port int) error {
	perm := m.findNetworkPermission(direction, protocol, host, port)
	if perm == nil {
		return m.deny(ctx, agentID, PermissionDescriptor{
			Type:     PermissionTypeNetwork,
			Action:   fmt.Sprintf("net:%s:%s:%s:%d", direction, protocol, host, port),
			Resource: host,
		}, "network scope missing")
	}
	if perm.HITLRequired {
		if err := m.ensureGrant(ctx, agentID, PermissionDescriptor{
			Type:         PermissionTypeNetwork,
			Action:       fmt.Sprintf("net:%s:%s", direction, protocol),
			Resource:     fmt.Sprintf("%s:%d", host, port),
			RequiresHITL: true,
		}); err != nil {
			return err
		}
	}
	m.log(ctx, agentID, PermissionDescriptor{
		Type:     PermissionTypeNetwork,
		Action:   fmt.Sprintf("net:%s", direction),
		Resource: fmt.Sprintf("%s:%d", host, port),
	}, "granted", nil)
	m.recordNetworkRule(direction, protocol, host, port)
	return nil
}

// recordNetworkRule stores approved network scopes and forwards them to the
// sandbox runtime so OS-level enforcement mirrors permission checks.
func (m *PermissionManager) recordNetworkRule(direction, protocol, host string, port int) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	rule := NetworkRule{
		Direction: direction,
		Protocol:  protocol,
		Host:      host,
		Port:      port,
	}
	m.netPolicy = append(m.netPolicy, rule)
	if m.runtime != nil {
		_ = m.runtime.EnforcePolicy(SandboxPolicy{
			NetworkRules: append([]NetworkRule(nil), m.netPolicy...),
		})
	}
}

// CheckCapability verifies capability usage.
func (m *PermissionManager) CheckCapability(ctx context.Context, agentID string, capability string) error {
	if !m.hasCapability(capability) {
		return m.deny(ctx, agentID, PermissionDescriptor{
			Type:     PermissionTypeCapability,
			Action:   fmt.Sprintf("cap:%s", capability),
			Resource: capability,
		}, "capability not declared")
	}
	m.log(ctx, agentID, PermissionDescriptor{
		Type:     PermissionTypeCapability,
		Action:   fmt.Sprintf("cap:%s", capability),
		Resource: capability,
	}, "granted", nil)
	return nil
}

// CheckIPC validates IPC usage.
func (m *PermissionManager) CheckIPC(ctx context.Context, agentID string, kind string, target string) error {
	perm := m.findIPCPermission(kind, target)
	if perm == nil {
		return m.deny(ctx, agentID, PermissionDescriptor{
			Type:     PermissionTypeIPC,
			Action:   fmt.Sprintf("ipc:%s", kind),
			Resource: target,
		}, "ipc scope missing")
	}
	if perm.HITLRequired {
		if err := m.ensureGrant(ctx, agentID, PermissionDescriptor{
			Type:         PermissionTypeIPC,
			Action:       fmt.Sprintf("ipc:%s", kind),
			Resource:     perm.Target,
			RequiresHITL: true,
		}); err != nil {
			return err
		}
	}
	m.log(ctx, agentID, PermissionDescriptor{
		Type:     PermissionTypeIPC,
		Action:   fmt.Sprintf("ipc:%s", kind),
		Resource: target,
	}, "granted", nil)
	return nil
}

// ensureSubset verifies a tool-declared permission set is a subset of the
// manifest permissions before the tool is callable.
func (m *PermissionManager) ensureSubset(requirements *PermissionSet) error {
	// Default deny posture ensures every tool scope must be included.
	check := func(action FileSystemAction, path string) bool {
		return m.findFilesystemPermission(action, path) != nil
	}
	for _, perm := range requirements.FileSystem {
		if !check(perm.Action, perm.Path) {
			return fmt.Errorf("fs permission %s %s missing", perm.Action, perm.Path)
		}
	}
	for _, exec := range requirements.Executables {
		if m.findExecutablePermission(exec.Binary) == nil {
			return fmt.Errorf("exec %s not allowed", exec.Binary)
		}
	}
	for _, net := range requirements.Network {
		if m.findNetworkPermission(net.Direction, net.Protocol, net.Host, net.Port) == nil {
			return fmt.Errorf("network %s %s missing", net.Direction, net.Host)
		}
	}
	return nil
}

// normalizePath sanitizes user input by resolving relative segments and
// preventing traversal outside the workspace base.
func (m *PermissionManager) normalizePath(path string) (string, error) {
	clean := filepath.Clean(path)
	clean = filepath.ToSlash(clean)
	if strings.HasPrefix(clean, "../") || strings.Contains(clean, "/../") {
		return "", fmt.Errorf("path traversal detected: %s", path)
	}
	if strings.HasPrefix(clean, "..") && clean != ".." {
		return "", fmt.Errorf("path traversal detected: %s", path)
	}
	if filepath.IsAbs(clean) {
		return clean, nil
	}
	if m.basePath == "" {
		return clean, nil
	}
	return filepath.ToSlash(filepath.Join(m.basePath, clean)), nil
}

// findFilesystemPermission returns the first filesystem permission matching the
// requested action/path pair.
func (m *PermissionManager) findFilesystemPermission(action FileSystemAction, path string) *FileSystemPermission {
	if m == nil || m.declared == nil {
		return nil
	}
	normalized := filepath.ToSlash(filepath.Clean(path))
	for _, perm := range m.declared.FileSystem {
		if perm.Action != action {
			continue
		}
		if matchGlob(perm.Path, normalized) {
			return &perm
		}
	}
	return nil
}

// findExecutablePermission locates the manifest entry authorizing a binary.
func (m *PermissionManager) findExecutablePermission(binary string) *ExecutablePermission {
	if m == nil || m.declared == nil {
		return nil
	}
	for _, perm := range m.declared.Executables {
		if perm.Binary == binary {
			return &perm
		}
	}
	return nil
}

// findNetworkPermission resolves whether the host/port pair is authorized for
// the given direction/protocol combination.
func (m *PermissionManager) findNetworkPermission(direction, protocol, host string, port int) *NetworkPermission {
	if m == nil || m.declared == nil {
		return nil
	}
	target := fmt.Sprintf("%s:%d", host, port)
	for _, perm := range m.declared.Network {
		if perm.Direction != direction || perm.Protocol != protocol {
			continue
		}
		if perm.Direction == "egress" {
			if perm.Port != 0 && perm.Port != port {
				continue
			}
			if perm.Host == host || perm.Host == permissionMatchAll || matchGlob(perm.Host, host) {
				return &perm
			}
		} else if perm.Direction == "ingress" {
			if perm.Port == port || perm.Port == 0 {
				return &perm
			}
		} else if perm.Direction == "dns" && perm.Host == "" {
			return &perm
		}
		if perm.Host == target {
			return &perm
		}
	}
	return nil
}

// findIPCPermission determines if the IPC target was declared in the manifest.
func (m *PermissionManager) findIPCPermission(kind, target string) *IPCPermission {
	if m == nil || m.declared == nil {
		return nil
	}
	for _, perm := range m.declared.IPC {
		if perm.Kind == kind && (perm.Target == target || perm.Target == permissionMatchAll) {
			return &perm
		}
	}
	return nil
}

// hasCapability checks whether a Linux capability was granted to the agent.
func (m *PermissionManager) hasCapability(cap string) bool {
	if m == nil || m.declared == nil {
		return false
	}
	for _, perm := range m.declared.Capabilities {
		if perm.Capability == cap {
			return true
		}
	}
	return false
}

// ensureGrant obtains a HITL approval when a permission requires human review.
func (m *PermissionManager) ensureGrant(ctx context.Context, agentID string, desc PermissionDescriptor) error {
	key := desc.Action + ":" + desc.Resource
	m.mu.Lock()
	if grant, ok := m.grants[key]; ok {
		if !grant.Expired(m.grantClock()) {
			m.mu.Unlock()
			return nil
		}
		delete(m.grants, key)
	}
	m.mu.Unlock()
	if m.hitl == nil {
		return m.deny(ctx, agentID, desc, "hitl approval required")
	}
	grant, err := m.hitl.RequestPermission(ctx, PermissionRequest{
		Permission:    desc,
		Justification: "runtime request",
		Scope:         GrantScopeSession,
		Risk:          RiskLevelMedium,
	})
	if err != nil {
		return err
	}
	m.mu.Lock()
	m.grants[key] = grant
	m.mu.Unlock()
	return nil
}

// deny records an audit event and returns a structured error describing why an
// action was blocked.
func (m *PermissionManager) deny(ctx context.Context, agentID string, desc PermissionDescriptor, reason string) error {
	m.log(ctx, agentID, desc, "denied", map[string]interface{}{
		"reason": reason,
	})
	return &PermissionDeniedError{
		Descriptor: desc,
		Message:    reason,
	}
}

// log forwards permission decisions to the configured audit sink to provide a
// tamper-evident trail of runtime behavior.
func (m *PermissionManager) log(ctx context.Context, agentID string, desc PermissionDescriptor, result string, fields map[string]interface{}) {
	if m.audit == nil {
		return
	}
	record := AuditRecord{
		Timestamp:   time.Now().UTC(),
		AgentID:     agentID,
		Action:      desc.Action,
		Type:        string(desc.Type),
		Permission:  desc.Resource,
		Result:      result,
		Metadata:    fields,
		Correlation: agentID,
	}
	_ = m.audit.Log(ctx, record)
}

// validateGlobPath enforces simple invariants on glob inputs to prevent agents
// from abusing globbing to escape the workspace root.
func validateGlobPath(path string) error {
	if path == "" {
		return errors.New("path empty")
	}
	if strings.Contains(path, "..") {
		return errors.New("path contains traversal sequence")
	}
	return nil
}

// matchGlob supports both filepath.Match and the '**' recursive glob pattern
// so manifests can succinctly describe directories.
func matchGlob(pattern, value string) bool {
	if pattern == permissionMatchAll {
		return true
	}
	pattern = filepath.ToSlash(pattern)
	value = filepath.ToSlash(value)
	if !strings.Contains(pattern, "**") {
		ok, err := filepath.Match(pattern, value)
		if err != nil {
			return false
		}
		return ok
	}
	regexPattern := globToRegex(pattern)
	regex, err := regexp.Compile(regexPattern)
	if err != nil {
		return false
	}
	return regex.MatchString(value)
}

// globToRegex converts '**' style globs into Go regular expressions so we can
// cheaply support recursive directory matching.
func globToRegex(pattern string) string {
	var b strings.Builder
	b.WriteString("^")
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		ch := runes[i]
		switch ch {
		case '*':
			peek := ""
			if i+1 < len(runes) {
				peek = string(runes[i+1])
			}
			if peek == "*" {
				b.WriteString(".*")
				i++
			} else {
				b.WriteString("[^/]*")
			}
		case '?':
			b.WriteString(".")
		case '.', '+', '(', ')', '|', '^', '$', '[', ']', '{', '}', '\\':
			b.WriteRune('\\')
			b.WriteRune(ch)
		default:
			b.WriteRune(ch)
		}
	}
	b.WriteString("$")
	return b.String()
}

// PermissionRequirement declares a permission needed by a tool or plugin.
type PermissionRequirement struct {
	Type     PermissionType
	Action   string
	Resource string
}

// ToolPermissions summarises tool requirements.
type ToolPermissions struct {
	Permissions *PermissionSet
}

// Validate ensures tool permission manifests are well-formed.
func (t ToolPermissions) Validate() error {
	if t.Permissions == nil {
		return errors.New("tool permissions missing")
	}
	return t.Permissions.Validate()
}

// HITLProvider handles human approvals.
type HITLProvider interface {
	RequestPermission(ctx context.Context, req PermissionRequest) (*PermissionGrant, error)
}

// PermissionGrant captures approval metadata.
type PermissionGrant struct {
	ID          string
	Permission  PermissionDescriptor
	Scope       GrantScope
	ExpiresAt   time.Time
	ApprovedBy  string
	Conditions  map[string]string
	GrantedAt   time.Time
	Description string
}

// Expired returns true when the grant is not usable anymore.
func (g *PermissionGrant) Expired(now time.Time) bool {
	if g == nil {
		return true
	}
	if g.ExpiresAt.IsZero() {
		return false
	}
	return now.After(g.ExpiresAt)
}

// matchArgs compares declared argument patterns with a runtime invocation while
// supporting simple globbing for flags.
func matchArgs(patterns, args []string) bool {
	if len(patterns) == 0 {
		return true
	}
	if len(patterns) != len(args) {
		return false
	}
	for i, pattern := range patterns {
		if pattern == "*" {
			continue
		}
		if strings.HasPrefix(pattern, "--") && strings.HasSuffix(pattern, "*") {
			if !strings.HasPrefix(args[i], strings.TrimSuffix(pattern, "*")) {
				return false
			}
			continue
		}
		if pattern != args[i] {
			return false
		}
	}
	return true
}

// matchEnv verifies required environment variables match the expected values or
// contain wildcards where any value is acceptable.
func matchEnv(patterns, env []string) bool {
	if len(patterns) == 0 {
		return true
	}
	m := map[string]string{}
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			m[parts[0]] = parts[1]
		}
	}
	for _, pattern := range patterns {
		parts := strings.SplitN(pattern, "=", 2)
		if len(parts) != 2 {
			continue
		}
		val, ok := m[parts[0]]
		if !ok {
			return false
		}
		if parts[1] != "*" && parts[1] != val {
			return false
		}
	}
	return true
}

// Sort normalizes permissions for deterministic manifests.
func (p *PermissionSet) Sort() {
	sort.Slice(p.FileSystem, func(i, j int) bool {
		return p.FileSystem[i].Path < p.FileSystem[j].Path
	})
	sort.Slice(p.Executables, func(i, j int) bool {
		return p.Executables[i].Binary < p.Executables[j].Binary
	})
	sort.Slice(p.Network, func(i, j int) bool {
		return p.Network[i].Host < p.Network[j].Host
	})
	sort.Slice(p.Capabilities, func(i, j int) bool {
		return p.Capabilities[i].Capability < p.Capabilities[j].Capability
	})
	sort.Slice(p.IPC, func(i, j int) bool {
		return p.IPC[i].Target < p.IPC[j].Target
	})
}
