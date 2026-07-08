// Package config parses undump.yaml into typed structs.
//
// Secrets (S3 keys, cloud token) are never stored in the YAML in plaintext —
// fields accept "env:VAR_NAME" references, which are resolved at load time.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// S3Source — dump source: S3.
type S3Source struct {
	Type        string `yaml:"type"`
	URI         string `yaml:"uri"`
	EndpointURL string `yaml:"endpoint_url"`
	AccessKey   string `yaml:"access_key"`
	SecretKey   string `yaml:"secret_key"`
	Region      string `yaml:"region"`
	// Pattern — optional glob (matched against the object's basename, e.g.
	// "*.dump"), narrows the candidates when picking the latest object by
	// prefix. Valid only when URI is a prefix (ends with "/").
	Pattern string `yaml:"pattern"`
}

// CheckConfig — config for a single check. Fields are the union across all
// check types (rowcount/freshness/sql_assert); Type determines which apply.
type CheckConfig struct {
	Type        string  `yaml:"type"`
	Table       string  `yaml:"table"`
	Column      string  `yaml:"column"`
	MaxAgeHours float64 `yaml:"max_age_hours"`
	MaxDropPct  float64 `yaml:"max_drop_pct"`
	ID          string  `yaml:"id"`
	Query       string  `yaml:"query"`
	Expect      string  `yaml:"expect"`
}

// Target — one backup under test.
type Target struct {
	Name     string        `yaml:"name"`
	Engine   string        `yaml:"engine"`
	Source   S3Source      `yaml:"source"`
	Schedule string        `yaml:"schedule"`
	Checks   []CheckConfig `yaml:"checks"`
}

// CloudConfig — where to send reports and with which key.
type CloudConfig struct {
	Endpoint string `yaml:"endpoint"`
	APIKey   string `yaml:"api_key"`
}

// Config — the agent's root config (undump.yaml).
type Config struct {
	Cloud   CloudConfig `yaml:"cloud"`
	Targets []Target    `yaml:"targets"`
}

// Load reads and parses undump.yaml, resolving "env:VAR" in secret fields.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading config %s: %w", path, err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parsing YAML %s: %w", path, err)
	}

	if cfg.Cloud.APIKey, err = resolveEnv(cfg.Cloud.APIKey); err != nil {
		return nil, fmt.Errorf("cloud.api_key: %w", err)
	}

	for i := range cfg.Targets {
		src := &cfg.Targets[i].Source
		if src.AccessKey, err = resolveEnv(src.AccessKey); err != nil {
			return nil, fmt.Errorf("targets[%d].source.access_key: %w", i, err)
		}
		if src.SecretKey, err = resolveEnv(src.SecretKey); err != nil {
			return nil, fmt.Errorf("targets[%d].source.secret_key: %w", i, err)
		}
		if src.Pattern != "" && !strings.HasSuffix(src.URI, "/") {
			return nil, fmt.Errorf("targets[%d].source.pattern: pattern is only valid when source.uri is a prefix (must end with \"/\")", i)
		}
	}

	return &cfg, nil
}

// resolveEnv turns "env:FOO" into the value of os.Getenv("FOO"); otherwise returns the value as-is.
func resolveEnv(value string) (string, error) {
	const prefix = "env:"
	if !strings.HasPrefix(value, prefix) {
		return value, nil
	}
	name := strings.TrimPrefix(value, prefix)
	resolved, ok := os.LookupEnv(name)
	if !ok {
		return "", fmt.Errorf("environment variable %q is not set (required by config)", name)
	}
	return resolved, nil
}
