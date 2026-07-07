// Package checks dispatches configured checks against a restored database.
//
// Phase 2 checks (rowcount/freshness/sql_assert) plug in here. Until a check
// type is registered, callers get ErrNotImplemented and can preserve the
// current "parsed but not executed" behavior.
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
	DSN    string
	Engine string
}

// Runner executes one configured check against the restored database.
type Runner interface {
	Run(context.Context, Context, config.CheckConfig) (models.CheckResult, error)
}

// RunnerFunc adapts a function to Runner.
type RunnerFunc func(context.Context, Context, config.CheckConfig) (models.CheckResult, error)

func (f RunnerFunc) Run(ctx context.Context, checkCtx Context, cfg config.CheckConfig) (models.CheckResult, error) {
	return f(ctx, checkCtx, cfg)
}

// Registry maps check types from undump.yaml to their implementations.
type Registry struct {
	runners map[string]Runner
}

func NewRegistry() *Registry {
	return &Registry{runners: map[string]Runner{}}
}

func (r *Registry) Register(checkType string, runner Runner) {
	r.runners[checkType] = runner
}

func (r *Registry) Run(ctx context.Context, checkCtx Context, cfg config.CheckConfig) (models.CheckResult, error) {
	runner, ok := r.runners[cfg.Type]
	if !ok {
		return models.CheckResult{}, fmt.Errorf("%w: %s", ErrNotImplemented, cfg.Type)
	}
	return runner.Run(ctx, checkCtx, cfg)
}

var DefaultRegistry = NewRegistry()

func Run(ctx context.Context, checkCtx Context, cfg config.CheckConfig) (models.CheckResult, error) {
	return DefaultRegistry.Run(ctx, checkCtx, cfg)
}
