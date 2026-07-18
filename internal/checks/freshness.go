package checks

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"undump/internal/config"
	"undump/internal/models"
)

func runFreshness(ctx context.Context, checkCtx Context, cfg config.CheckConfig) (models.CheckResult, error) {
	if strings.TrimSpace(cfg.Table) == "" {
		return models.CheckResult{}, fmt.Errorf("freshness.table is required")
	}
	if strings.TrimSpace(cfg.Column) == "" {
		return models.CheckResult{}, fmt.Errorf("freshness.column is required")
	}
	if cfg.MaxAgeHours <= 0 {
		return models.CheckResult{}, fmt.Errorf("freshness.max_age_hours must be > 0")
	}
	if checkCtx.QueryScalar == nil {
		return models.CheckResult{}, fmt.Errorf("freshness requires a query runner")
	}

	// Let the database calculate age; timestamp text formats vary by engine.
	var query string
	switch checkCtx.Engine {
	case "postgres", "":
		query = fmt.Sprintf("SELECT EXTRACT(EPOCH FROM (now() - MAX(%s))) FROM %s", cfg.Column, cfg.Table)
	case "mysql":
		query = fmt.Sprintf("SELECT TIMESTAMPDIFF(SECOND, MAX(%s), NOW()) FROM %s", cfg.Column, cfg.Table)
	default:
		return models.CheckResult{}, fmt.Errorf("freshness supports postgres/mysql, got engine %q", checkCtx.Engine)
	}

	raw, err := checkCtx.QueryScalar(ctx, query)
	if err != nil {
		return models.CheckResult{}, fmt.Errorf("querying max %s.%s: %w", cfg.Table, cfg.Column, err)
	}

	expected := fmt.Sprintf("<= %g", cfg.MaxAgeHours)
	result := models.CheckResult{Name: "freshness", Expected: &expected}

	// The clients represent MAX() over an empty table differently.
	if raw == "" || strings.EqualFold(raw, "NULL") {
		result.Status = models.CheckStatusFail
		result.Detail = fmt.Sprintf("%s.%s has no timestamp values (empty table?)", cfg.Table, cfg.Column)
		return result, nil
	}

	ageSeconds, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return models.CheckResult{}, fmt.Errorf("unexpected age output %q for %s.%s: %w", raw, cfg.Table, cfg.Column, err)
	}
	ageHours := ageSeconds / 3600

	value := fmt.Sprintf("%.2f", ageHours)
	result.Value = &value
	result.Detail = fmt.Sprintf("newest %s.%s is %.1fh old (max %gh)", cfg.Table, cfg.Column, ageHours, cfg.MaxAgeHours)
	result.Status = models.CheckStatusFail
	if ageHours <= cfg.MaxAgeHours {
		result.Status = models.CheckStatusPass
	}
	return result, nil
}
