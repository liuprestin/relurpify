package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// newConfigCmd registers subcommands that inspect or mutate config.yaml.
func newConfigCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "config",
		Short: "Inspect or modify config.yaml",
	}
	cmd.AddCommand(newConfigGetCmd(), newConfigSetCmd())
	return cmd
}

// newConfigGetCmd prints the value referenced by a dotted key.
func newConfigGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get [key]",
		Short: "Read a config value by dotted key",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readConfigMap(cfgFile)
			if err != nil {
				return err
			}
			value, ok := getConfigValue(data, args[0])
			if !ok {
				return fmt.Errorf("key %s not found", args[0])
			}
			fmt.Fprintln(cmd.OutOrStdout(), prettyValue(value))
			return nil
		},
	}
}

// newConfigSetCmd updates a dotted key with the provided value.
func newConfigSetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "set [key] [value]",
		Short: "Update a config value",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			data, err := readConfigMap(cfgFile)
			if err != nil {
				return err
			}
			if err := setConfigValue(data, args[0], parseValue(args[1])); err != nil {
				return err
			}
			if err := writeConfigMap(cfgFile, data); err != nil {
				return err
			}
			fmt.Fprintf(cmd.OutOrStdout(), "%s updated\n", args[0])
			return nil
		},
	}
}
