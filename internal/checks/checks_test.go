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

func TestRunReturnsErrNotImplementedForUnknownCheck(t *testing.T) {
	_, err := Run(context.Background(), Context{}, config.CheckConfig{Type: "unknown"})

	require.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotImplemented))
	assert.Contains(t, err.Error(), "unknown")
}

func TestSQLAssertPassesAgainstRestoredPostgres(t *testing.T) {
	ctx := context.Background()
	session, err := dockerengine.Restore(ctx, "../../testdata/sample_plain.sql")
	require.NoError(t, err)
	defer func() { assert.NoError(t, session.Close()) }()

	result, err := Run(ctx, Context{Engine: "postgres", QueryScalar: session.QueryScalar}, config.CheckConfig{
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

	result, err := Run(ctx, Context{Engine: "postgres", QueryScalar: session.QueryScalar}, config.CheckConfig{
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

	checkCtx := Context{Engine: session.EngineName(), QueryScalar: session.QueryScalar}

	result, err := Run(ctx, checkCtx, config.CheckConfig{Type: "rowcount", Table: "widgets"})
	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusPass, result.Status)
	assert.Equal(t, "3", *result.Value)

	prev := int64(4)
	checkCtx.LastRowcount = &prev
	result, err = Run(ctx, checkCtx, config.CheckConfig{Type: "rowcount", Table: "widgets"})
	require.NoError(t, err)
	assert.Equal(t, models.CheckStatusFail, result.Status)

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

	checkCtx := Context{Engine: session.EngineName(), QueryScalar: session.QueryScalar}

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

	result, err := Run(ctx, Context{Engine: "mysql", QueryScalar: session.QueryScalar}, config.CheckConfig{
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
