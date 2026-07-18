package checks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/config"
	"undump/internal/models"
)

func fakeScalar(t *testing.T, wantQuery, reply string) func(context.Context, string) (string, error) {
	t.Helper()
	return func(_ context.Context, query string) (string, error) {
		assert.Equal(t, wantQuery, query)
		return reply, nil
	}
}

func ptrInt64(v int64) *int64 { return &v }

func TestRowcountWithoutPreviousValueRecordsBaselineAndPasses(t *testing.T) {
	checkCtx := Context{QueryScalar: fakeScalar(t, "SELECT COUNT(*) FROM widgets", "42")}

	result, err := Run(context.Background(), checkCtx, config.CheckConfig{Type: "rowcount", Table: "widgets"})

	require.NoError(t, err)
	assert.Equal(t, "rowcount", result.Name)
	assert.Equal(t, models.CheckStatusPass, result.Status)
	assert.Equal(t, "42", *result.Value)
	assert.Nil(t, result.Expected)
	assert.Contains(t, result.Detail, "baseline")
}

func TestRowcountPassesWhenDropIsWithinThreshold(t *testing.T) {
	checkCtx := Context{
		QueryScalar:  fakeScalar(t, "SELECT COUNT(*) FROM widgets", "950"),
		LastRowcount: ptrInt64(1000),
	}

	result, err := Run(context.Background(), checkCtx, config.CheckConfig{Type: "rowcount", Table: "widgets", MaxDropPct: 10})

	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusPass, result.Status)
	assert.Equal(t, "950", *result.Value)
	assert.Equal(t, ">= 900", *result.Expected)
	assert.Contains(t, result.Detail, "-5.0%")
}

func TestRowcountFailsWhenDropExceedsThreshold(t *testing.T) {
	checkCtx := Context{
		QueryScalar:  fakeScalar(t, "SELECT COUNT(*) FROM widgets", "500"),
		LastRowcount: ptrInt64(1000),
	}

	result, err := Run(context.Background(), checkCtx, config.CheckConfig{Type: "rowcount", Table: "widgets", MaxDropPct: 10})

	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusFail, result.Status)
	assert.Equal(t, "500", *result.Value)
	assert.Equal(t, ">= 900", *result.Expected)
	assert.Contains(t, result.Detail, "-50.0%")
}

func TestRowcountGrowthAlwaysPasses(t *testing.T) {
	checkCtx := Context{
		QueryScalar:  fakeScalar(t, "SELECT COUNT(*) FROM widgets", "1100"),
		LastRowcount: ptrInt64(1000),
	}

	result, err := Run(context.Background(), checkCtx, config.CheckConfig{Type: "rowcount", Table: "widgets", MaxDropPct: 10})

	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusPass, result.Status)
	assert.Contains(t, result.Detail, "+10.0%")
}

func TestRowcountUsesDefaultThresholdWhenUnset(t *testing.T) {
	// SPEC §6: default threshold is a 10% drop. 89 < 90 → fail.
	checkCtx := Context{
		QueryScalar:  fakeScalar(t, "SELECT COUNT(*) FROM widgets", "89"),
		LastRowcount: ptrInt64(100),
	}

	result, err := Run(context.Background(), checkCtx, config.CheckConfig{Type: "rowcount", Table: "widgets"})

	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusFail, result.Status)
	assert.Equal(t, ">= 90", *result.Expected)
}

func TestRowcountRequiresTable(t *testing.T) {
	_, err := Run(context.Background(), Context{}, config.CheckConfig{Type: "rowcount"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "rowcount.table")
}

func TestRowcountRejectsUnparsableCount(t *testing.T) {
	checkCtx := Context{QueryScalar: fakeScalar(t, "SELECT COUNT(*) FROM widgets", "oops")}

	_, err := Run(context.Background(), checkCtx, config.CheckConfig{Type: "rowcount", Table: "widgets"})

	require.Error(t, err)
	assert.Contains(t, err.Error(), `"oops"`)
}
