package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"undump/internal/checks"
	"undump/internal/config"
	"undump/internal/dockerengine"
	"undump/internal/models"
	"undump/internal/reportclient"
	"undump/internal/sources"
)

const version = "0.3.0" // x-release-please-version

func main() {
	root := &cobra.Command{
		Use:     "undump",
		Short:   "UnDump — continuous backup restore testing",
		Version: version,
	}
	root.AddCommand(newCheckCmd())
	root.AddCommand(newRunCmd())

	if err := root.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newCheckCmd() *cobra.Command {
	var configPath string
	cmd := &cobra.Command{
		Use:   "check",
		Short: "Run a single pass over all targets in the config",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runCheck(cmd.Context(), configPath)
		},
	}
	cmd.Flags().StringVar(&configPath, "config", "", "path to undump.yaml")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

func runCheck(ctx context.Context, configPath string) error {
	cfg, err := config.Load(configPath)
	if err != nil {
		return fmt.Errorf("loading config: %w", err)
	}

	for _, target := range cfg.Targets {
		// Only the daemon carries rowcount baselines between runs.
		report := runTarget(ctx, target, nil)
		logReport(report)
		reportclient.Send(ctx, cfg.Cloud.Endpoint, cfg.Cloud.APIKey, report)
	}
	return nil
}

func logReport(report models.RunReport) {
	fmt.Printf("[%-5s] %s rto=%v checks=%d\n",
		strings.ToUpper(string(report.Status)), report.TargetName, rtoString(report.RTOSeconds), len(report.Checks))
	if report.Error != nil {
		fmt.Printf("  error: %s\n", *report.Error)
	}
	for _, c := range report.Checks {
		if c.Status != models.CheckStatusPass {
			fmt.Printf("  %s: %s\n", c.Name, c.Detail)
		}
	}
}

// runTarget acquires, restores, and checks one target. Operational errors are
// recorded in the report so another target can still run.
func runTarget(ctx context.Context, target config.Target, lastRowcount *int64) models.RunReport {
	started := time.Now().UTC()
	report := models.RunReport{
		TargetName:   target.Name,
		Engine:       target.Engine,
		SourceURI:    sources.ReportURI(target.Source),
		AgentVersion: version,
		Checks:       []models.CheckResult{},
	}

	tmpDir, err := prepareAcquisitionDest(target.Source)
	if err != nil {
		return finalizeError(report, started, fmt.Errorf("creating temp directory: %w", err))
	}
	if tmpDir != "" {
		defer func() {
			if rmErr := os.RemoveAll(tmpDir); rmErr != nil {
				slog.Warn("failed to remove temp directory", "path", tmpDir, "error", rmErr)
			}
		}()
	}

	artifact, err := sources.Acquire(ctx, target.Source, tmpDir)
	if err != nil {
		phase := "acquiring dump"
		if target.Source.Type == "s3" {
			phase = "downloading dump"
		}
		return finalizeError(report, started, fmt.Errorf("%s: %w", phase, err))
	}
	report.DumpSizeBytes = &artifact.Size
	report.SourceURI = artifact.ReportURI

	session, err := dockerengine.Restore(ctx, artifact.Path)
	if err != nil {
		return finalizeRuntimeError(
			report,
			started,
			target.Source,
			"restoring local backup: failed",
			fmt.Errorf("restoring: %w", err),
		)
	}
	defer func() {
		if cerr := session.Close(); cerr != nil {
			slog.Warn("failed to remove ephemeral container", "target", target.Name, "error", cerr)
		}
	}()

	rto := session.Outcome.RTOSeconds
	report.RTOSeconds = &rto

	checkStatus := models.CheckStatusFail
	if session.Outcome.OK {
		checkStatus = models.CheckStatusPass
	}
	report.Checks = append(report.Checks, models.CheckResult{
		Name:   "restore",
		Status: checkStatus,
		Detail: session.Outcome.Detail,
	})

	if session.Outcome.OK {
		// Checks use the detected engine; target.Engine is only a report label.
		checkCtx := checks.Context{
			Engine:       session.EngineName(),
			QueryScalar:  session.QueryScalar,
			LastRowcount: lastRowcount,
		}
		for _, c := range target.Checks {
			if c.Type == "restore" {
				continue
			}
			result, err := checks.Run(ctx, checkCtx, c)
			if err != nil {
				if errors.Is(err, checks.ErrNotImplemented) {
					slog.Info("unsupported check skipped", "type", c.Type, "target", target.Name)
					continue
				}
				return finalizeRuntimeError(
					report,
					started,
					target.Source,
					fmt.Sprintf("running local backup check %s: failed", c.Type),
					fmt.Errorf("running check %s: %w", c.Type, err),
				)
			}
			report.Checks = append(report.Checks, result)
		}
	}

	report.Status = statusFromChecks(report.Checks)
	report.StartedAt = started
	report.FinishedAt = time.Now().UTC()
	return report
}

func prepareAcquisitionDest(src config.SourceConfig) (string, error) {
	if src.Type != "s3" {
		return "", nil
	}
	return os.MkdirTemp("", "undump-")
}

func finalizeRuntimeError(
	report models.RunReport,
	started time.Time,
	src config.SourceConfig,
	localMessage string,
	err error,
) models.RunReport {
	if src.Type == "local" {
		err = errors.New(localMessage)
	}
	return finalizeError(report, started, err)
}

func finalizeError(report models.RunReport, started time.Time, err error) models.RunReport {
	msg := err.Error()
	report.Error = &msg
	report.Status = models.StatusError
	report.StartedAt = started
	report.FinishedAt = time.Now().UTC()
	return report
}

func rtoString(rto *float64) string {
	if rto == nil {
		return "-"
	}
	return fmt.Sprintf("%.2fs", *rto)
}

func statusFromChecks(results []models.CheckResult) models.Status {
	for _, result := range results {
		if result.Status != models.CheckStatusPass {
			return models.StatusFail
		}
	}
	return models.StatusPass
}
