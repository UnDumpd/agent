package checks

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"undump/internal/config"
	"undump/internal/models"
)

// Zero means the default because max_drop_pct is not a pointer in the YAML model.
const defaultMaxDropPct = 10.0

func runRowcount(ctx context.Context, checkCtx Context, cfg config.CheckConfig) (models.CheckResult, error) {
	if strings.TrimSpace(cfg.Table) == "" {
		return models.CheckResult{}, fmt.Errorf("rowcount.table is required")
	}
	if cfg.MaxDropPct < 0 {
		return models.CheckResult{}, fmt.Errorf("rowcount.max_drop_pct must be >= 0")
	}
	if checkCtx.QueryScalar == nil {
		return models.CheckResult{}, fmt.Errorf("rowcount requires a query runner")
	}

	raw, err := checkCtx.QueryScalar(ctx, "SELECT COUNT(*) FROM "+cfg.Table)
	if err != nil {
		return models.CheckResult{}, fmt.Errorf("counting rows in %s: %w", cfg.Table, err)
	}
	count, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return models.CheckResult{}, fmt.Errorf("unexpected COUNT(*) output %q for %s: %w", raw, cfg.Table, err)
	}

	value := strconv.FormatInt(count, 10)
	result := models.CheckResult{Name: "rowcount", Value: &value}

	if checkCtx.LastRowcount == nil {
		result.Status = models.CheckStatusPass
		result.Detail = fmt.Sprintf("%s: %d rows (baseline, no previous value)", cfg.Table, count)
		return result, nil
	}

	prev := *checkCtx.LastRowcount
	maxDrop := cfg.MaxDropPct
	if maxDrop == 0 {
		maxDrop = defaultMaxDropPct
	}
	minAllowed := int64(math.Ceil(float64(prev) * (1 - maxDrop/100)))
	expected := fmt.Sprintf(">= %d", minAllowed)
	result.Expected = &expected

	changePct := 0.0
	if prev != 0 {
		changePct = (float64(count) - float64(prev)) / float64(prev) * 100
	}
	result.Detail = fmt.Sprintf("%s: %d rows (%+.1f%% vs previous %d)", cfg.Table, count, changePct, prev)
	result.Status = models.CheckStatusFail
	if count >= minAllowed {
		result.Status = models.CheckStatusPass
	}
	return result, nil
}
