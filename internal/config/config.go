// Package config loads undump.yaml and resolves env: references in secret fields.
package config

import (
	"fmt"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// S3Source describes a dump stored in S3-compatible storage.
type S3Source struct {
	Type        string `yaml:"type"`
	URI         string `yaml:"uri"`
	EndpointURL string `yaml:"endpoint_url"`
	AccessKey   string `yaml:"access_key"`
	SecretKey   string `yaml:"secret_key"`
	Region      string `yaml:"region"`
	// Pattern filters object basenames when URI points to a prefix.
	Pattern string `yaml:"pattern"`
}

// CheckConfig contains the fields used by all supported check types.
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

// Target is one backup under test.
type Target struct {
	Name     string        `yaml:"name"`
	Engine   string        `yaml:"engine"`
	Source   S3Source      `yaml:"source"`
	Schedule string        `yaml:"schedule"`
	Checks   []CheckConfig `yaml:"checks"`
}

// DefaultCloudEndpoint is used when an API key is set without an endpoint.
const DefaultCloudEndpoint = "https://api.undumpd.com"

// CloudConfig contains report delivery settings.
type CloudConfig struct {
	Endpoint string `yaml:"endpoint"`
	APIKey   string `yaml:"api_key"`
}

// Config is the root of undump.yaml.
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
	if cfg.Cloud.Endpoint == "" && cfg.Cloud.APIKey != "" {
		cfg.Cloud.Endpoint = DefaultCloudEndpoint
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
