package cmd

import (
	"errors"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/agents"
)

var (
	cfgFile   string
	workspace string

	globalCfg *agents.GlobalConfig
)

// Execute is the entry point for the CLI.
func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// NewRootCmd wires the cobra tree.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "coding-agent",
		Short:         "Multi-mode coding agent orchestrator",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			if workspace == "" {
				if wd, err := os.Getwd(); err == nil {
					workspace = wd
				} else {
					return err
				}
			}
			if cfgFile == "" {
				cfgFile = agents.DefaultConfigPath(workspace)
			}
			cfg, err := agents.LoadGlobalConfig(cfgFile, workspace)
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
			globalCfg = cfg
			return nil
		},
	}
	root.PersistentFlags().StringVar(&workspace, "workspace", "", "Workspace directory")
	root.PersistentFlags().StringVar(&cfgFile, "config", "", "Path to coding-agent config file")

	root.AddCommand(
		newStartCmd(),
		newAgentsCmd(),
		newConfigCmd(),
		newSessionCmd(),
	)
	return root
}
