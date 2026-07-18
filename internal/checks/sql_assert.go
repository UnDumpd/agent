package checks

import (
	"context"
	"fmt"
	"strings"

	"undump/internal/config"
	"undump/internal/models"
)

func runSQLAssert(ctx context.Context, checkCtx Context, cfg config.CheckConfig) (models.CheckResult, error) {
	if cfg.ID == "" {
		return models.CheckResult{}, fmt.Errorf("sql_assert.id is required")
	}
	if strings.TrimSpace(cfg.Query) == "" {
		return models.CheckResult{}, fmt.Errorf("sql_assert.query is required")
	}

	if checkCtx.QueryScalar == nil {
		return models.CheckResult{}, fmt.Errorf("sql_assert requires a query runner")
	}

	value, err := checkCtx.QueryScalar(ctx, cfg.Query)
	if err != nil {
		return models.CheckResult{}, fmt.Errorf("running sql_assert %s: %w", cfg.ID, err)
	}
	expected := cfg.Expect
	status := models.CheckStatusFail
	detail := fmt.Sprintf("expected %q, got %q", expected, value)
	if value == expected {
		status = models.CheckStatusPass
		detail = fmt.Sprintf("value matched %q", expected)
	}

	return models.CheckResult{
		Name:     "sql_assert:" + cfg.ID,
		Status:   status,
		Detail:   detail,
		Value:    &value,
		Expected: &expected,
	}, nil
}
