package main

import (
	"encoding/json"
	"errors"
	"os"

	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/framework"
)

func newInspectCmd() *cobra.Command {
	var contextPath string
	cmd := &cobra.Command{
		Use:   "inspect",
		Short: "Inspect a stored context JSON file",
		RunE: func(cmd *cobra.Command, args []string) error {
			if contextPath == "" {
				return errors.New("context file path required")
			}
			data, err := os.ReadFile(contextPath)
			if err != nil {
				return err
			}
			var ctx framework.Context
			if err := json.Unmarshal(data, &ctx); err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(ctx)
		},
	}
	cmd.Flags().StringVar(&contextPath, "file", "", "Path to a JSON context snapshot")
	return cmd
}
