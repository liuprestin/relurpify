package framework

// RestrictToolRegistryByMatrix removes tools that are disabled by the manifest's
// coarse tool matrix. This is separate from PermissionManager enforcement: the
// matrix determines which tools an agent can *see*, while permissions determine
// whether a visible tool is allowed to execute.
func RestrictToolRegistryByMatrix(registry *ToolRegistry, matrix AgentToolMatrix) {
	if registry == nil {
		return
	}
	allowed := make([]string, 0)
	for _, tool := range registry.All() {
		if toolAllowedByMatrix(tool, matrix) {
			allowed = append(allowed, tool.Name())
		}
	}
	registry.RestrictTo(allowed)
}

func toolAllowedByMatrix(tool Tool, matrix AgentToolMatrix) bool {
	perms := tool.Permissions().Permissions
	if perms != nil {
		if permissionRequiresFileRead(perms) && !matrix.FileRead {
			return false
		}
		if permissionRequiresFileWrite(perms) && !matrix.FileWrite {
			return false
		}
		if permissionRequiresExecute(perms) && !matrix.BashExecute {
			return false
		}
		if permissionRequiresNetwork(perms) && !matrix.WebSearch {
			return false
		}
	}

	switch tool.Category() {
	case "lsp":
		return matrix.LSPQuery
	case "search":
		return matrix.SearchCodebase
	case "git", "execution":
		return matrix.BashExecute
	default:
		return true
	}
}

func permissionRequiresFileRead(perms *PermissionSet) bool {
	for _, fs := range perms.FileSystem {
		if fs.Action == FileSystemRead || fs.Action == FileSystemList {
			return true
		}
	}
	return false
}

func permissionRequiresFileWrite(perms *PermissionSet) bool {
	for _, fs := range perms.FileSystem {
		if fs.Action == FileSystemWrite {
			return true
		}
	}
	return false
}

func permissionRequiresExecute(perms *PermissionSet) bool {
	for _, fs := range perms.FileSystem {
		if fs.Action == FileSystemExecute {
			return true
		}
	}
	return len(perms.Executables) > 0
}

func permissionRequiresNetwork(perms *PermissionSet) bool {
	return len(perms.Network) > 0
}

