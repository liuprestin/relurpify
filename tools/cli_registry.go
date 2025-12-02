package tools

import (
	"github.com/lexcodex/relurpify/framework"
	cliarchive "github.com/lexcodex/relurpify/tools/cli_nix/archive"
	clibuild "github.com/lexcodex/relurpify/tools/cli_nix/build"
	clifileops "github.com/lexcodex/relurpify/tools/cli_nix/fileops"
	clinetwork "github.com/lexcodex/relurpify/tools/cli_nix/network"
	clischeduler "github.com/lexcodex/relurpify/tools/cli_nix/scheduler"
	clisystem "github.com/lexcodex/relurpify/tools/cli_nix/system"
	clitext "github.com/lexcodex/relurpify/tools/cli_nix/text"
)

// CommandLineTools exposes the default Unix-style CLI helpers.
func CommandLineTools(basePath string, runner framework.CommandRunner) []framework.Tool {
	var res []framework.Tool
	res = append(res, clitext.Tools(basePath)...)
	res = append(res, clifileops.Tools(basePath)...)
	res = append(res, clisystem.Tools(basePath)...)
	res = append(res, clibuild.Tools(basePath)...)
	res = append(res, cliarchive.Tools(basePath)...)
	res = append(res, clinetwork.Tools(basePath)...)
	res = append(res, clischeduler.Tools(basePath)...)
	for i, tool := range res {
		if setter, ok := tool.(interface{ SetCommandRunner(framework.CommandRunner) }); ok {
			setter.SetCommandRunner(runner)
			res[i] = tool
		}
	}
	return res
}
