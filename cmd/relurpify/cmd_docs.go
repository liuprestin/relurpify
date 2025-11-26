package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

func newDocsCmd() *cobra.Command {
	var docsDir string
	var goldsBin string
	cmd := &cobra.Command{
		Use:   "docs",
		Short: "Generate static documentation using Golds",
		RunE: func(cmd *cobra.Command, args []string) error {
			if goldsBin == "" {
				gopath := os.Getenv("GOPATH")
				if gopath == "" {
					var err error
					gopath, err = os.UserHomeDir()
					if err != nil {
						return err
					}
					gopath = filepath.Join(gopath, "go")
				}
				goldsBin = filepath.Join(gopath, "bin", "golds")
			}
			if _, err := os.Stat(goldsBin); err != nil {
				return fmt.Errorf("golds binary not found (%s)", goldsBin)
			}
			cacheDir := filepath.Join(flagWorkspace, ".gocache")
			modDir := filepath.Join(flagWorkspace, ".gomodcache")
			if err := os.MkdirAll(docsDir, 0o755); err != nil {
				return err
			}
			env := append(os.Environ(), "GOCACHE="+cacheDir, "GOMODCACHE="+modDir)
			command := exec.Command(goldsBin, "-gen", "-dir="+docsDir, "./...")
			command.Dir = flagWorkspace
			command.Env = env
			command.Stdout = cmd.OutOrStdout()
			command.Stderr = cmd.ErrOrStderr()
			return command.Run()
		},
	}
	cmd.Flags().StringVar(&docsDir, "out", filepath.Join(flagWorkspace, "docs"), "Output directory for docs")
	cmd.Flags().StringVar(&goldsBin, "golds", "", "Path to golds binary (default GOPATH/bin/golds)")
	return cmd
}
