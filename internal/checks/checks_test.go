package checks

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/config"
	"undump/internal/dockerengine"
	"undump/internal/models"
)

func TestRegistryRunReturnsErrNotImplementedForUnknownCheck(t *testing.T) {
	registry := NewRegistry()

	_, err := registry.Run(context.Background(), Context{}, config.CheckConfig{Type: "rowcount"})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotImplemented))
	assert.Contains(t, err.Error(), "rowcount")
}

func TestRegistryRunDispatchesRegisteredCheck(t *testing.T) {
	registry := NewRegistry()
	registry.Register("rowcount", RunnerFunc(func(ctx context.Context, checkCtx Context, cfg config.CheckConfig) (models.CheckResult, error) {
		assert.Equal(t, "postgresql://example", checkCtx.DSN)
		assert.Equal(t, "postgres", checkCtx.Engine)
		assert.Equal(t, "users", cfg.Table)
		return models.CheckResult{Name: "rowcount", Status: models.CheckStatusPass, Detail: "42 rows"}, nil
	}))

	result, err := registry.Run(
		context.Background(),
		Context{DSN: "postgresql://example", Engine: "postgres"},
		config.CheckConfig{Type: "rowcount", Table: "users"},
	)

	require.NoError(t, err)
	assert.Equal(t, "rowcount", result.Name)
	assert.Equal(t, models.CheckStatusPass, result.Status)
	assert.Equal(t, "42 rows", result.Detail)
}

func TestSQLAssertPassesAgainstRestoredPostgres(t *testing.T) {
	ctx := context.Background()
	session, err := dockerengine.Restore(ctx, "../../testdata/sample_plain.sql")
	require.NoError(t, err)
	defer func() { assert.NoError(t, session.Close()) }()

	result, err := Run(ctx, Context{DSN: session.DSN, Engine: "postgres", QueryScalar: session.QueryScalar}, config.CheckConfig{
		Type:   "sql_assert",
		ID:     "widget_count",
		Query:  "SELECT count(*) FROM widgets",
		Expect: "3",
	})

	require.NoError(t, err)
	assert.Equal(t, "sql_assert:widget_count", result.Name)
	assert.Equal(t, models.CheckStatusPass, result.Status)
	assert.Equal(t, "3", *result.Value)
	assert.Equal(t, "3", *result.Expected)
}

func TestSQLAssertFailsWhenValueDiffers(t *testing.T) {
	ctx := context.Background()
	session, err := dockerengine.Restore(ctx, "../../testdata/sample_plain.sql")
	require.NoError(t, err)
	defer func() { assert.NoError(t, session.Close()) }()

	result, err := Run(ctx, Context{DSN: session.DSN, Engine: "postgres", QueryScalar: session.QueryScalar}, config.CheckConfig{
		Type:   "sql_assert",
		ID:     "widget_count",
		Query:  "SELECT count(*) FROM widgets",
		Expect: "4",
	})

	require.NoError(t, err)
	assert.Equal(t, "sql_assert:widget_count", result.Name)
	assert.Equal(t, models.CheckStatusFail, result.Status)
	assert.Equal(t, "3", *result.Value)
	assert.Equal(t, "4", *result.Expected)
}

func TestRowcountAndFreshnessAgainstRestoredPostgres(t *testing.T) {
	ctx := context.Background()
	session, err := dockerengine.Restore(ctx, "../../testdata/sample_plain.sql")
	require.NoError(t, err)
	defer func() { assert.NoError(t, session.Close()) }()

	// EngineName() instead of a literal: freshness must follow the engine
	// detected from the dump, same as cmd/undump wires it
	checkCtx := Context{DSN: session.DSN, Engine: session.EngineName(), QueryScalar: session.QueryScalar}

	// rowcount: no previous value → baseline pass
	result, err := Run(ctx, checkCtx, config.CheckConfig{Type: "rowcount", Table: "widgets"})
	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusPass, result.Status)
	assert.Equal(t, "3", *result.Value)

	// rowcount: 3 rows vs previous 4 is a -25% drop → fail at the default 10%
	prev := int64(4)
	checkCtx.LastRowcount = &prev
	result, err = Run(ctx, checkCtx, config.CheckConfig{Type: "rowcount", Table: "widgets"})
	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusFail, result.Status)

	// freshness: fixture rows are from 2026-07-01 — young enough for 10 years,
	// too old for ~0.4 seconds
	result, err = Run(ctx, checkCtx, config.CheckConfig{Type: "freshness", Table: "widgets", Column: "created_at", MaxAgeHours: 87600})
	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusPass, result.Status)

	result, err = Run(ctx, checkCtx, config.CheckConfig{Type: "freshness", Table: "widgets", Column: "created_at", MaxAgeHours: 0.0001})
	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusFail, result.Status)
}

func TestRowcountAndFreshnessAgainstRestoredMySQL(t *testing.T) {
	ctx := context.Background()
	session, err := dockerengine.Restore(ctx, "../../testdata/sample_mysql.sql")
	require.NoError(t, err)
	defer func() { assert.NoError(t, session.Close()) }()

	checkCtx := Context{DSN: session.DSN, Engine: session.EngineName(), QueryScalar: session.QueryScalar}

	result, err := Run(ctx, checkCtx, config.CheckConfig{Type: "rowcount", Table: "widgets"})
	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusPass, result.Status)
	assert.Equal(t, "3", *result.Value)

	result, err = Run(ctx, checkCtx, config.CheckConfig{Type: "freshness", Table: "widgets", Column: "created_at", MaxAgeHours: 87600})
	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusPass, result.Status)
}

func TestSQLAssertPassesAgainstRestoredMySQL(t *testing.T) {
	ctx := context.Background()
	session, err := dockerengine.Restore(ctx, "../../testdata/sample_mysql.sql")
	require.NoError(t, err)
	defer func() { assert.NoError(t, session.Close()) }()

	result, err := Run(ctx, Context{DSN: session.DSN, Engine: "mysql", QueryScalar: session.QueryScalar}, config.CheckConfig{
		Type:   "sql_assert",
		ID:     "widget_count",
		Query:  "SELECT count(*) FROM widgets",
		Expect: "3",
	})

	require.NoError(t, err)
	assert.Equal(t, "sql_assert:widget_count", result.Name)
	assert.Equal(t, models.CheckStatusPass, result.Status)
	assert.Equal(t, "3", *result.Value)
	assert.Equal(t, "3", *result.Expected)
}
