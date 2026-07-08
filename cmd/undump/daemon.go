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

// runDaemon loads the config once and schedules each target on its own cron
// expression. Unlike "check", it never returns on its own — it blocks until
// SIGINT/SIGTERM, then waits for any in-flight target run to finish before
// exiting, so a restore in progress always gets to clean up its container.
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
	// SkipIfStillRunning: a restore can easily outlast a tight schedule
	// (large databases, slow S3) — without it, a slow target would pile up
	// concurrent restores against the same Docker host instead of just
	// running a bit late.
	sched := cron.New(cron.WithChain(cron.SkipIfStillRunning(cron.DiscardLogger)))
	for _, target := range cfg.Targets {
		target := target
		if _, err := sched.AddFunc(target.Schedule, func() {
			// Runs use a background context, not the daemon's shutdown ctx:
			// a signal arriving mid-restore must not abort the container
			// cleanup that runTarget's defers guarantee. Cancellation via
			// ctx.Done() below only stops SCHEDULING new runs.
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
