package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/framework"
)

func newMemoryCmd() *cobra.Command {
	var baseDir string
	memoryCmd := &cobra.Command{
		Use:   "memory",
		Short: "Inspect hybrid memory",
	}
	memoryCmd.PersistentFlags().StringVar(&baseDir, "dir", filepath.Join(flagWorkspace, ".memory"), "Memory directory")

	recallCmd := &cobra.Command{
		Use:   "recall",
		Short: "Recall a memory by scope and key",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return errors.New("usage: memory recall <scope> <key>")
			}
			store, err := framework.NewHybridMemory(baseDir)
			if err != nil {
				return err
			}
			scope := framework.MemoryScope(strings.ToLower(args[0]))
			rec, ok, err := store.Recall(cmd.Context(), args[1], scope)
			if err != nil {
				return err
			}
			if !ok {
				return fmt.Errorf("memory %s not found", args[1])
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(rec)
		},
	}

	searchCmd := &cobra.Command{
		Use:   "search",
		Short: "Search memories by query",
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(args) < 2 {
				return errors.New("usage: memory search <scope> <query>")
			}
			store, err := framework.NewHybridMemory(baseDir)
			if err != nil {
				return err
			}
			scope := framework.MemoryScope(strings.ToLower(args[0]))
			results, err := store.Search(cmd.Context(), args[1], scope)
			if err != nil {
				return err
			}
			enc := json.NewEncoder(cmd.OutOrStdout())
			enc.SetIndent("", "  ")
			return enc.Encode(results)
		},
	}

	memoryCmd.AddCommand(recallCmd, searchCmd)
	return memoryCmd
}
