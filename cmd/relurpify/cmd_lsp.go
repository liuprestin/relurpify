package main

import (
	"context"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/lexcodex/relurpify/cmd/internal/cliutils"
)

func newLSPCmd() *cobra.Command {
	var root string
	var language string
	var file string
	cmd := &cobra.Command{
		Use:   "lsp",
		Short: "Probe an LSP server configuration",
		RunE: func(cmd *cobra.Command, args []string) error {
			desc, ok := cliutils.LookupLSPDescriptor(language)
			if !ok {
				return fmt.Errorf("unsupported language %s", language)
			}
			client, err := desc.Factory(root)
			if err != nil {
				return err
			}
			defer func() {
				if closer, ok := client.(interface{ Close() error }); ok {
					_ = closer.Close()
				}
			}()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			if file != "" {
				diags, err := client.GetDiagnostics(ctx, file)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "Diagnostics: %v\n", diags)
			}
			symbols, err := client.SearchSymbols(ctx, "")
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "workspace/symbol error: %v\n", err)
			} else if len(symbols) > 0 {
				fmt.Fprintf(cmd.OutOrStdout(), "Symbol sample: %v\n", symbols[:min(5, len(symbols))])
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&root, "root", flagWorkspace, "Workspace root for language server")
	cmd.Flags().StringVar(&language, "lang", "go", "Language (go,rust,clangd,haskell,ts,lua,python)")
	cmd.Flags().StringVar(&file, "file", "", "Optional file to request diagnostics for")
	return cmd
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
