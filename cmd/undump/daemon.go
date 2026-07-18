package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/robfig/cron/v3"
	"github.com/spf13/cobra"

	"undump/internal/config"
	"undump/internal/reportclient"
)

func newRunCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "run",
		Short: "Run as a daemon, checking each target on its own cron schedule",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDaemon(cmd.Context(), configPath)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to undump.yaml")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

// runDaemon schedules targets until its context or a process signal is done.
func runDaemon(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}
	if len(cfg.Targets) == 0 {
		return fmt.Errorf("no targets configured")
	}
	for _, target := range cfg.Targets {
		if strings.TrimSpace(target.Schedule) == "" {
			return fmt.Errorf("target %q: schedule is required for \"run\" (\"check\" ignores it, but the daemon needs it)", target.Name)
		}
	}

	ctx, stopSignals := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stopSignals()

	state := newRowcountState()
	// Do not overlap restores of the same target.
	sched := cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)))
	for _, target := range cfg.Targets {
		if _, err := sched.AddFunc(target.Schedule, func() {
			// Shutdown stops scheduling but lets an active restore clean up.
			runAndReport(context.Background(), cfg.Cloud, target, state)
		}); err != nil {
			return fmt.Errorf("target %q: invalid schedule %q: %w", target.Name, target.Schedule, err)
		}
		slog.Info("scheduled target", "target", target.Name, "schedule", target.Schedule)
	}

	sched.Start()
	slog.Info("undump daemon started", "targets", len(cfg.Targets))

	<-ctx.Done()
	slog.Info("shutdown signal received, waiting for in-flight runs to finish")
	<-sched.Stop().Done()
	slog.Info("undump daemon stopped")
	return nil
}

func runAndReport(ctx context.Context, cloud config.CloudConfig, target config.Target, state *rowcountState) {
	report := runTarget(ctx, target, state.get(target.Name))
	logReport(report)
	resp, ok := reportclient.Send(ctx, cloud.Endpoint, cloud.APIKey, report)
	if ok {
		state.set(target.Name, resp.LastRowcount)
	}
}
