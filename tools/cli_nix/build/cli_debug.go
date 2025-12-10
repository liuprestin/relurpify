package build

import (
	"github.com/lexcodex/relurpify/framework"
	clinix "github.com/lexcodex/relurpify/tools/cli_nix"
)

// NewGDBTool creates a GDB debugger wrapper.
func NewGDBTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:         "cli_gdb",
		Description:  "GNU Debugger.",
		Command:      "gdb",
		Category:     "cli_debug",
		HITLRequired: true,
	})
}

// NewValgrindTool creates a Valgrind wrapper.
func NewValgrindTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_valgrind",
		Description: "Valgrind instrumentation framework (memcheck, cachegrind, etc).",
		Command:     "valgrind",
		Category:    "cli_debug",
	})
}

// NewLddTool creates an ldd wrapper.
func NewLddTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_ldd",
		Description: "Print shared object dependencies.",
		Command:     "ldd",
		Category:    "cli_debug",
	})
}

// NewObjdumpTool creates an objdump wrapper.
func NewObjdumpTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:        "cli_objdump",
		Description: "Display information from object files.",
		Command:     "objdump",
		Category:    "cli_debug",
	})
}

// NewPerfTool creates a perf wrapper.
func NewPerfTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:         "cli_perf",
		Description:  "Performance analysis tools for Linux.",
		Command:      "perf",
		Category:     "cli_debug",
		HITLRequired: true,
	})
}

// NewStraceTool creates a strace wrapper.
func NewStraceTool(basePath string) framework.Tool {
	return clinix.NewCommandTool(basePath, clinix.CommandToolConfig{
		Name:         "cli_strace",
		Description:  "Trace system calls and signals.",
		Command:      "strace",
		Category:     "cli_debug",
		HITLRequired: true,
	})
}
