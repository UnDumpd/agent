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
