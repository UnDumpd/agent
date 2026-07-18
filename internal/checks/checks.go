// Package checks runs configured checks against a restored database.
package checks

import (
	"context"
	"errors"
	"fmt"

	"undump/internal/config"
	"undump/internal/models"
)

var ErrNotImplemented = errors.New("check is not implemented")

// Context is the stable input shared by all configured checks.
type Context struct {
	Engine      string
	QueryScalar func(context.Context, string) (string, error)
	// LastRowcount is nil until the cloud returns a baseline for this target.
	LastRowcount *int64
}

// Run dispatches a configured check.
func Run(ctx context.Context, checkCtx Context, cfg config.CheckConfig) (models.CheckResult, error) {
	switch cfg.Type {
	case "sql_assert":
		return runSQLAssert(ctx, checkCtx, cfg)
	case "rowcount":
		return runRowcount(ctx, checkCtx, cfg)
	case "freshness":
		return runFreshness(ctx, checkCtx, cfg)
	default:
		return models.CheckResult{}, fmt.Errorf("%w: %s", ErrNotImplemented, cfg.Type)
	}
}
