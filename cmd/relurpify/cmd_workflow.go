package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"time"

	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/persistence"
)

func newWorkflowCmd() *cobra.Command {
	var storePath string
	workflowCmd := &cobra.Command{
		Use:   "workflow",
		Short: "Inspect workflow snapshots",
	}
	workflowCmd.PersistentFlags().StringVar(&storePath, "store", filepath.Join(flagWorkspace, ".control"), "Workflow store location")

	listCmd := &cobra.Command{
		Use:   "list",
		Short: "List known workflow snapshots",
		RunE: func(cmd *cobra.Command, args []string) error {
			store, err := persistence.NewFileWorkflowStore(storePath)
			if err != nil {
				return err
			}
			snapshots, err := store.List(cmd.Context())
			if err != nil {
				return err
			}
			for _, snap := range snapshots {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%s\n", snap.ID, snap.Status, snap.UpdatedAt.Format(time.RFC3339))
			}
			return nil
		},
	}

	showCmd := &cobra.Command{
		Use:   "show",
		Short: "Show a workflow snapshot by ID",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("workflow id required")
			}
			store, err := persistence.NewFileWorkflowStore(storePath)
			if err != nil {
				return err
			}
			snap, ok, err := store.Load(cmd.Context(), args[0])
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("snapshot %s not found", args[0])
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(snap)
		},
	}

	deleteCmd := &cobra.Command{
		Use:   "delete",
		Short: "Delete a workflow snapshot",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) == 0 {
				return errors.New("workflow id required")
			}
			store, err := persistence.NewFileWorkflowStore(storePath)
			if err != nil {
				return err
			}
			return store.Delete(cmd.Context(), args[0])
		},
	}

	workflowCmd.AddCommand(listCmd, showCmd, deleteCmd)
	return workflowCmd
}
