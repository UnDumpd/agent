package checks

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/config"
	"undump/internal/models"
)

func freshnessCfg() config.CheckConfig {
	return config.CheckConfig{Type: "freshness", Table: "widgets", Column: "created_at", MaxAgeHours: 24}
}

func TestFreshnessPassesWhenNewestRowIsYoungEnough(t *testing.T) {
	// 12 hours in seconds against max_age_hours=24.
	checkCtx := Context{
		Engine:      "postgres",
		QueryScalar: fakeScalar(t, "SELECT EXTRACT(EPOCH FROM (now() - MAX(created_at))) FROM widgets", "43200.5"),
	}

	result, err := Run(context.Background(), checkCtx, freshnessCfg())

	require.NoError(t, err)
	assert.Equal(t, "freshness", result.Name)
	assert.Equal(t, models.CheckStatusPass, result.Status)
	assert.Equal(t, "12.00", *result.Value)
	assert.Equal(t, "<= 24", *result.Expected)
}

func TestFreshnessFailsWhenNewestRowIsTooOld(t *testing.T) {
	// 48 hours in seconds against max_age_hours=24.
	checkCtx := Context{
		Engine:      "postgres",
		QueryScalar: fakeScalar(t, "SELECT EXTRACT(EPOCH FROM (now() - MAX(created_at))) FROM widgets", "172800"),
	}

	result, err := Run(context.Background(), checkCtx, freshnessCfg())

	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusFail, result.Status)
	assert.Equal(t, "48.00", *result.Value)
	assert.Contains(t, result.Detail, "48.0h old (max 24h)")
}

func TestFreshnessUsesMySQLQueryForMySQLEngine(t *testing.T) {
	checkCtx := Context{
		Engine:      "mysql",
		QueryScalar: fakeScalar(t, "SELECT TIMESTAMPDIFF(SECOND, MAX(created_at), NOW()) FROM widgets", "3600"),
	}

	result, err := Run(context.Background(), checkCtx, freshnessCfg())

	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusPass, result.Status)
	assert.Equal(t, "1.00", *result.Value)
}

func TestFreshnessFailsOnEmptyTable(t *testing.T) {
	for _, reply := range []string{"", "NULL"} {
		checkCtx := Context{
			Engine:      "postgres",
			QueryScalar: fakeScalar(t, "SELECT EXTRACT(EPOCH FROM (now() - MAX(created_at))) FROM widgets", reply),
		}

		result, err := Run(context.Background(), checkCtx, freshnessCfg())

		require.NoError(t, err)
		assert.Equal(t, models.CheckStatusFail, result.Status)
		assert.Nil(t, result.Value)
		assert.Contains(t, result.Detail, "no timestamp values")
	}
}

func TestFreshnessRejectsUnsupportedEngine(t *testing.T) {
	checkCtx := Context{Engine: "mongo", QueryScalar: fakeScalar(t, "unused", "")}

	_, err := Run(context.Background(), checkCtx, freshnessCfg())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "mongo")
}

func TestFreshnessValidatesConfig(t *testing.T) {
	cases := []struct {
		name string
		cfg  config.CheckConfig
		want string
	}{
		{"missing table", config.CheckConfig{Type: "freshness", Column: "ts", MaxAgeHours: 1}, "freshness.table"},
		{"missing column", config.CheckConfig{Type: "freshness", Table: "t", MaxAgeHours: 1}, "freshness.column"},
		{"missing max_age", config.CheckConfig{Type: "freshness", Table: "t", Column: "ts"}, "max_age_hours"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Run(context.Background(), Context{}, tc.cfg)

			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}
