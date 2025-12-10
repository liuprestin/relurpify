package testsuite

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/tools"
)

func TestFileToolGranularPermissionEnforcement(t *testing.T) {
	base := t.TempDir()
	if err := os.WriteFile(filepath.Join(base, "secret.txt"), []byte("secret data"), 0o600); err != nil {
		t.Fatalf("failed to create secret file: %v", err)
	}
	
	// Create permission set that requires HITL for everything
	perms := framework.NewFileSystemPermissionSet(base, framework.FileSystemRead)
	for i := range perms.FileSystem {
		perms.FileSystem[i].HITLRequired = true
	}
	
	manager, err := framework.NewPermissionManager(base, perms, nil, nil) // No HITL provider -> Fail
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}

	registry := framework.NewToolRegistry()
	readTool := &tools.ReadFileTool{BasePath: base}
	if err := registry.Register(readTool); err != nil {
		t.Fatalf("register tool: %v", err)
	}
	
	registry.UsePermissionManager("test-agent", manager)
	
	tool, ok := registry.Get("file_read")
	if !ok {
		t.Fatalf("tool missing")
	}

	// Attempt to read secret.txt
	// AuthorizeTool should pass (Agent has base/**, Tool has base/**)
	// CheckFileAccess should fail (HITL required but missing provider)
	
	ctx := context.Background()
	state := framework.NewContext()
	
	_, err = tool.Execute(ctx, state, map[string]interface{}{"path": "secret.txt"})
	if err == nil {
		t.Fatal("expected HITL error, got success")
	}
	if !strings.Contains(err.Error(), "hitl approval required") {
		t.Fatalf("expected hitl approval required error, got: %v", err)
	}
}



func TestWriteToolBackupPermissionEnforcement(t *testing.T) {
	base := t.TempDir()
	
	// Permission to write everything, BUT with HITL
	perms := framework.NewFileSystemPermissionSet(base, framework.FileSystemWrite)
	for i := range perms.FileSystem {
		perms.FileSystem[i].HITLRequired = true
	}
	
	manager, err := framework.NewPermissionManager(base, perms, nil, nil) // No HITL provider -> Fail
	if err != nil {
		t.Fatalf("manager init failed: %v", err)
	}

	registry := framework.NewToolRegistry()
	writeTool := &tools.WriteFileTool{BasePath: base, Backup: true}
	registry.Register(writeTool)
	registry.UsePermissionManager("test-agent", manager)
	
	tool, _ := registry.Get("file_write")

	ctx := context.Background()
	state := framework.NewContext()
	
	// Execute write. Should trigger HITL and fail.
	_, err = tool.Execute(ctx, state, map[string]interface{}{"path": "config.json", "content": "v1"})
	if err == nil {
		t.Fatal("expected HITL error, got success")
	}
	if !strings.Contains(err.Error(), "hitl approval required") {
		t.Fatalf("expected hitl approval required error, got: %v", err)
	}
}
