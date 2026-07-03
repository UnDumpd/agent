package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"undump/internal/config"
)

const sampleYAML = `
cloud:
  endpoint: "https://cloud.undump.dev"
  api_key: "env:TEST_UNDUMP_API_KEY"

targets:
  - name: "prod-billing"
    engine: "postgres"
    schedule: "0 * * * *"
    source:
      type: "s3"
      uri: "s3://backups/billing/latest.dump"
      endpoint_url: "http://minio:9000"
      access_key: "env:TEST_S3_ACCESS_KEY"
      secret_key: "plain-secret-not-env"
    checks:
      - type: "rowcount"
        table: "invoices"
        max_drop_pct: 10.0
`

func writeTempConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "undump.yaml")
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
	return path
}

func TestLoad_ParsesTargetsAndResolvesEnv(t *testing.T) {
	t.Setenv("TEST_UNDUMP_API_KEY", "secret-api-key")
	t.Setenv("TEST_S3_ACCESS_KEY", "resolved-access-key")

	path := writeTempConfig(t, sampleYAML)
	cfg, err := config.Load(path)
	require.NoError(t, err)

	assert.Equal(t, "secret-api-key", cfg.Cloud.APIKey)
	require.Len(t, cfg.Targets, 1)

	target := cfg.Targets[0]
	assert.Equal(t, "prod-billing", target.Name)
	assert.Equal(t, "resolved-access-key", target.Source.AccessKey)
	assert.Equal(t, "plain-secret-not-env", target.Source.SecretKey, "values without the env: prefix are left as-is")
	require.Len(t, target.Checks, 1)
	assert.Equal(t, "rowcount", target.Checks[0].Type)
	assert.Equal(t, "invoices", target.Checks[0].Table)
}

func TestLoad_MissingEnvVarFails(t *testing.T) {
	path := writeTempConfig(t, `
cloud:
  endpoint: "https://cloud.undump.dev"
  api_key: "env:TEST_VAR_DEFINITELY_NOT_SET"
targets: []
`)
	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "TEST_VAR_DEFINITELY_NOT_SET")
}

func TestLoad_PatternRequiresPrefix(t *testing.T) {
	t.Setenv("TEST_UNDUMP_API_KEY", "secret-api-key")
	path := writeTempConfig(t, `
cloud:
  endpoint: "https://cloud.undump.dev"
  api_key: "env:TEST_UNDUMP_API_KEY"
targets:
  - name: "prod-billing"
    engine: "postgres"
    schedule: "0 * * * *"
    source:
      type: "s3"
      uri: "s3://backups/billing/latest.dump"
      access_key: "plain-access-key"
      secret_key: "plain-secret-key"
      pattern: "*.dump"
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "source.pattern")
}

func TestLoad_ParsesPatternField(t *testing.T) {
	t.Setenv("TEST_UNDUMP_API_KEY", "secret-api-key")
	path := writeTempConfig(t, `
cloud:
  endpoint: "https://cloud.undump.dev"
  api_key: "env:TEST_UNDUMP_API_KEY"
targets:
  - name: "prod-billing"
    engine: "postgres"
    schedule: "0 * * * *"
    source:
      type: "s3"
      uri: "s3://backups/billing/"
      access_key: "plain-access-key"
      secret_key: "plain-secret-key"
      pattern: "*.dump"
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Targets, 1)
	assert.Equal(t, "*.dump", cfg.Targets[0].Source.Pattern)
}

func TestLoad_RealExampleFile(t *testing.T) {
	// The real undump.example.yaml from the repo should parse without changes.
	t.Setenv("UNDUMP_API_KEY", "x")
	t.Setenv("S3_ACCESS_KEY", "x")
	t.Setenv("S3_SECRET_KEY", "x")

	cfg, err := config.Load("../../undump.example.yaml")
	require.NoError(t, err)
	assert.Len(t, cfg.Targets, 2)
	assert.Equal(t, "prod-billing", cfg.Targets[0].Name)
}
