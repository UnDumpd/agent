package config_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

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

func TestLoad_ParsesLocalExactFileSource(t *testing.T) {
	path := writeTempConfig(t, `
targets:
  - name: local
    source:
      type: local
      path: backups/latest.dump
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Targets, 1)
	assert.Equal(t, "local", cfg.Targets[0].Source.Type)
	assert.Equal(t, filepath.Join(filepath.Dir(path), "backups", "latest.dump"), cfg.Targets[0].Source.Path)
	assert.Equal(t, 5*time.Minute, cfg.Targets[0].Source.MinAge.Duration)
	assert.False(t, cfg.Targets[0].Source.MinAge.Set)
}

func TestLoad_ParsesLocalDirectorySource(t *testing.T) {
	path := writeTempConfig(t, `
targets:
  - name: local
    source:
      type: local
      path: backups
      pattern: "*.dump"
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Targets, 1)
	assert.Equal(t, filepath.Join(filepath.Dir(path), "backups"), cfg.Targets[0].Source.Path)
	assert.Equal(t, "*.dump", cfg.Targets[0].Source.Pattern)
}

func TestLoad_PreservesExplicitZeroLocalMinAge(t *testing.T) {
	path := writeTempConfig(t, `
targets:
  - name: local
    source:
      type: local
      path: /backups
      min_age: 0s
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Targets, 1)
	assert.Zero(t, cfg.Targets[0].Source.MinAge.Duration)
	assert.True(t, cfg.Targets[0].Source.MinAge.Set)
}

func TestLoad_RejectsInvalidLocalMinAge(t *testing.T) {
	path := writeTempConfig(t, `
targets:
  - name: local
    source:
      type: local
      path: /backups
      min_age: soon
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "min_age")
	assert.Contains(t, err.Error(), "invalid duration")
}

func TestLoad_RejectsNegativeLocalMinAge(t *testing.T) {
	path := writeTempConfig(t, `
targets:
  - name: local
    source:
      type: local
      path: /backups
      min_age: -1s
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "targets[0].source.min_age")
	assert.Contains(t, err.Error(), "must not be negative")
}

func TestLoad_DefaultsS3MinAge(t *testing.T) {
	t.Setenv("TEST_UNDUMP_API_KEY", "secret-api-key")
	path := writeTempConfig(t, `
cloud:
  endpoint: "https://cloud.undump.dev"
  api_key: "env:TEST_UNDUMP_API_KEY"
targets:
  - name: "s3-backup"
    engine: "postgres"
    schedule: "0 * * * *"
    source:
      type: "s3"
      uri: "s3://backups/latest.dump"
      access_key: "plain-access-key"
      secret_key: "plain-secret-key"
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Targets, 1)
	assert.Equal(t, 5*time.Minute, cfg.Targets[0].Source.MinAge.Duration)
	assert.False(t, cfg.Targets[0].Source.MinAge.Set)
}

func TestLoad_PreservesExplicitZeroS3MinAge(t *testing.T) {
	t.Setenv("TEST_UNDUMP_API_KEY", "secret-api-key")
	path := writeTempConfig(t, `
cloud:
  endpoint: "https://cloud.undump.dev"
  api_key: "env:TEST_UNDUMP_API_KEY"
targets:
  - name: "s3-backup"
    engine: "postgres"
    schedule: "0 * * * *"
    source:
      type: "s3"
      uri: "s3://backups/latest.dump"
      access_key: "plain-access-key"
      secret_key: "plain-secret-key"
      min_age: 0s
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Targets, 1)
	assert.Zero(t, cfg.Targets[0].Source.MinAge.Duration)
	assert.True(t, cfg.Targets[0].Source.MinAge.Set)
}

func TestLoad_RejectsNegativeS3MinAge(t *testing.T) {
	t.Setenv("TEST_UNDUMP_API_KEY", "secret-api-key")
	path := writeTempConfig(t, `
cloud:
  endpoint: "https://cloud.undump.dev"
  api_key: "env:TEST_UNDUMP_API_KEY"
targets:
  - name: "s3-backup"
    engine: "postgres"
    schedule: "0 * * * *"
    source:
      type: "s3"
      uri: "s3://backups/latest.dump"
      access_key: "plain-access-key"
      secret_key: "plain-secret-key"
      min_age: -1s
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "targets[0].source.min_age")
	assert.Contains(t, err.Error(), "must not be negative")
}

func TestLoad_RejectsMissingLocalPath(t *testing.T) {
	path := writeTempConfig(t, `
targets:
  - name: local
    source:
      type: local
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "targets[0].source.path")
	assert.Contains(t, err.Error(), "required")
}

func TestLoad_RejectsUnsupportedSourceType(t *testing.T) {
	path := writeTempConfig(t, `
targets:
  - name: remote
    source:
      type: ftp
`)

	_, err := config.Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "targets[0].source.type")
	assert.Contains(t, err.Error(), `"ftp"`)
}

func TestLoad_ResolvesRelativeLocalPathFromConfigDirectory(t *testing.T) {
	path := writeTempConfig(t, `
targets:
  - name: local
    source:
      type: local
      path: backups/latest.dump
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Targets, 1)
	expected, err := filepath.Abs(filepath.Join(filepath.Dir(path), "backups", "latest.dump"))
	require.NoError(t, err)
	assert.Equal(t, expected, cfg.Targets[0].Source.Path)
}

func TestLoad_DoesNotResolveS3CredentialsForLocalSource(t *testing.T) {
	path := writeTempConfig(t, `
targets:
  - name: local
    source:
      type: local
      path: /backups/latest.dump
      access_key: env:TEST_LOCAL_UNUSED_ACCESS_KEY
      secret_key: env:TEST_LOCAL_UNUSED_SECRET_KEY
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	require.Len(t, cfg.Targets, 1)
	assert.Equal(t, "env:TEST_LOCAL_UNUSED_ACCESS_KEY", cfg.Targets[0].Source.AccessKey)
	assert.Equal(t, "env:TEST_LOCAL_UNUSED_SECRET_KEY", cfg.Targets[0].Source.SecretKey)
}

func TestLoad_DefaultCloudEndpoint(t *testing.T) {
	t.Setenv("TEST_UNDUMP_API_KEY", "secret-api-key")

	tests := []struct {
		name     string
		cloud    string
		expected string
	}{
		{"api key without endpoint", "  api_key: env:TEST_UNDUMP_API_KEY\n", config.DefaultCloudEndpoint},
		{"no api key", "", ""},
		{"explicit endpoint", "  endpoint: https://cloud.example.com\n", "https://cloud.example.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			path := writeTempConfig(t, "cloud:\n"+tt.cloud+"targets: []\n")
			cfg, err := config.Load(path)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, cfg.Cloud.Endpoint)
		})
	}
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

func TestLoad_EmptyEnvVarIsValid(t *testing.T) {
	t.Setenv("TEST_EMPTY_API_KEY", "")
	path := writeTempConfig(t, `
cloud:
  api_key: "env:TEST_EMPTY_API_KEY"
targets: []
`)

	cfg, err := config.Load(path)
	require.NoError(t, err)
	assert.Empty(t, cfg.Cloud.APIKey)
	assert.Empty(t, cfg.Cloud.Endpoint)
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
	t.Setenv("S3_ACCESS_KEY", "x")
	t.Setenv("S3_SECRET_KEY", "x")

	cfg, err := config.Load("../../undump.example.yaml")
	require.NoError(t, err)
	assert.Empty(t, cfg.Cloud.APIKey)
	assert.Len(t, cfg.Targets, 5)
	assert.Equal(t, "prod-billing", cfg.Targets[0].Name)
	assert.Equal(t, "mysql", cfg.Targets[2].Engine)
	assert.Equal(t, "local-billing", cfg.Targets[3].Name)
	assert.Equal(t, "local", cfg.Targets[3].Source.Type)
	assert.Equal(t, 5*time.Minute, cfg.Targets[3].Source.MinAge.Duration)
	assert.Equal(t, "local-sessions-mongo", cfg.Targets[4].Name)
	assert.Equal(t, "mongo", cfg.Targets[4].Engine)
	assert.Equal(t, "/backups/mongo/sessions", cfg.Targets[4].Source.Path)
}
