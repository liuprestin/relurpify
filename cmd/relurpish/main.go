package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	runtimesvc "github.com/lexcodex/relurpify/internal/relurpish/runtime"
	"github.com/lexcodex/relurpify/internal/relurpish/tui"
)

var (
	cfg         = runtimesvc.DefaultConfig()
	startServer bool
)

func main() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	root := newRootCmd()
	if err := root.ExecuteContext(ctx); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "relurpish",
		Short:         "Bubble Tea shell for the Relurpify agent runtime",
		SilenceUsage:  true,
		SilenceErrors: false,
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			return cfg.Normalize()
		},
	}
	root.PersistentFlags().StringVar(&cfg.Workspace, "workspace", cfg.Workspace, "Workspace directory")
	root.PersistentFlags().StringVar(&cfg.ManifestPath, "manifest", cfg.ManifestPath, "Agent manifest path")
	root.PersistentFlags().StringVar(&cfg.OllamaEndpoint, "ollama-endpoint", cfg.OllamaEndpoint, "Ollama endpoint URL")
	root.PersistentFlags().StringVar(&cfg.OllamaModel, "ollama-model", cfg.OllamaModel, "Ollama model name")
	root.PersistentFlags().StringVar(&cfg.AgentName, "agent", cfg.AgentLabel(), "Agent preset (coding, planner, react, reflection)")
	root.PersistentFlags().StringVar(&cfg.ServerAddr, "addr", cfg.ServerAddr, "HTTP server listen address")
	root.PersistentFlags().StringVar(&cfg.Sandbox.RunscPath, "runsc", cfg.Sandbox.RunscPath, "runsc binary path")
	root.PersistentFlags().StringVar(&cfg.Sandbox.ContainerRuntime, "container-runtime", cfg.Sandbox.ContainerRuntime, "Container runtime (docker/containerd)")
	root.PersistentFlags().StringVar(&cfg.Sandbox.Platform, "sandbox-platform", cfg.Sandbox.Platform, "gVisor platform (kvm/ptrace)")
	root.PersistentFlags().BoolVar(&startServer, "serve", false, "Launch the HTTP API server alongside the TUI")

	root.AddCommand(newWizardCmd(), newStatusCmd(), newChatCmd(), newServeCmd())
	return root
}

func newWizardCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "wizard",
		Short: "Run the configuration wizard",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithRuntime(cmd, func(ctx context.Context, rt *runtimesvc.Runtime) error {
				return runTUI(ctx, rt, tui.ModeWizard)
			})
		},
	}
	return cmd
}

func newStatusCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show workspace diagnostics",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithRuntime(cmd, func(ctx context.Context, rt *runtimesvc.Runtime) error {
				return runTUI(ctx, rt, tui.ModeStatus)
			})
		},
	}
	return cmd
}

func newChatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat",
		Short: "Start the relurpish chat shell",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithRuntime(cmd, func(ctx context.Context, rt *runtimesvc.Runtime) error {
				return runTUI(ctx, rt, tui.ModeChat)
			})
		},
	}
	return cmd
}

func newServeCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run only the HTTP API server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithRuntime(cmd, func(cmdCtx context.Context, rt *runtimesvc.Runtime) error {
				stop, err := rt.StartServer(cmdCtx, cfg.ServerAddr)
				if err != nil {
					return err
				}
				fmt.Fprintf(cmd.OutOrStdout(), "relurpish API listening on %s\n", cfg.ServerAddr)
				<-cmdCtx.Done()
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				return stop(shutdownCtx)
			})
		},
	}
	return cmd
}

func runWithRuntime(cmd *cobra.Command, fn func(context.Context, *runtimesvc.Runtime) error) error {
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}
	rt, err := runtimesvc.New(ctx, cfg)
	if err != nil {
		return err
	}
	defer rt.Close()
	return fn(ctx, rt)
}

func runTUI(ctx context.Context, rt *runtimesvc.Runtime, mode tui.Mode) error {
	var stop func(context.Context) error
	var err error
	if startServer {
		stop, err = rt.StartServer(ctx, cfg.ServerAddr)
		if err != nil {
			return err
		}
		defer stop(context.Background())
	}
	return tui.Run(ctx, rt, cfg, tui.Options{InitialMode: mode})
}
