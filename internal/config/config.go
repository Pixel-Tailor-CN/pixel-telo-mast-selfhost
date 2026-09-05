package config

import (
	"bytes"
	"fmt"
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Duration time.Duration

func (d *Duration) UnmarshalYAML(value *yaml.Node) error {
	parsed, err := time.ParseDuration(value.Value)
	if err != nil {
		return fmt.Errorf("invalid duration %q: %w", value.Value, err)
	}
	*d = Duration(parsed)
	return nil
}

func (d Duration) Std() time.Duration { return time.Duration(d) }

type ProviderConfig struct {
	MinInterval    Duration `yaml:"min_interval"`
	MaxConcurrent  int      `yaml:"max_concurrent"`
	BreakerTimeout Duration `yaml:"breaker_timeout"`
	ProxyURL       string   `yaml:"proxy_url"`
}

type LogRotationConfig struct {
	MaxSizeMB int  `yaml:"max_size_mb"`
	Daily     bool `yaml:"daily"`
	LocalTime bool `yaml:"local_time"`
	Compress  bool `yaml:"compress"`
}

type LogRetentionConfig struct {
	MaxAge         Duration `yaml:"max_age"`
	MaxBackups     int      `yaml:"max_backups"`
	MaxTotalSizeMB int      `yaml:"max_total_size_mb"`
}

type LogConfig struct {
	Level     string             `yaml:"level"`
	Format    string             `yaml:"format"`
	Rotation  LogRotationConfig  `yaml:"rotation"`
	Retention LogRetentionConfig `yaml:"retention"`
}

func defaultLogConfig() LogConfig {
	return LogConfig{
		Level:  "info",
		Format: "json",
		Rotation: LogRotationConfig{
			MaxSizeMB: 100,
			Daily:     true,
			LocalTime: true,
			Compress:  true,
		},
		Retention: LogRetentionConfig{
			MaxAge:         Duration(30 * 24 * time.Hour),
			MaxBackups:     30,
			MaxTotalSizeMB: 1024,
		},
	}
}

func (c *Config) TLSFiles() (string, string) {
	certFile, keyFile := c.TLS.CertFile, c.TLS.KeyFile
	if certFile == "" {
		certFile = c.Storage.RuntimePath + ".crt"
	}
	if keyFile == "" {
		keyFile = c.Storage.RuntimePath + ".key"
	}
	return certFile, keyFile
}

type Config struct {
	Server struct {
		Listen string `yaml:"listen"`
	} `yaml:"server"`
	Auth struct {
		TokenFile string `yaml:"token_file"`
	} `yaml:"auth"`
	TLS struct {
		Mode                        string `yaml:"mode"`
		PublicURL                   string `yaml:"public_url"`
		CertFile                    string `yaml:"cert_file"`
		KeyFile                     string `yaml:"key_file"`
		AllowInsecurePrivateNetwork bool   `yaml:"allow_insecure_private_network"`
	} `yaml:"tls"`
	Storage struct {
		RuntimePath string `yaml:"runtime_path"`
	} `yaml:"storage"`
	Baseline struct {
		Enabled       bool     `yaml:"enabled"`
		SyncOnStart   bool     `yaml:"sync_on_start"`
		CheckInterval Duration `yaml:"check_interval"`
	} `yaml:"baseline"`
	Query struct {
		Timeout       Duration `yaml:"timeout"`
		MaxConcurrent int      `yaml:"max_concurrent"`
	} `yaml:"query"`
	RateLimit struct {
		RequestsPerSecond float64 `yaml:"requests_per_second"`
		Burst             int     `yaml:"burst"`
	} `yaml:"rate_limit"`
	Upstream struct {
		ProviderIDs []string `yaml:"provider_ids"`
	} `yaml:"upstream"`
	Providers map[string]ProviderConfig `yaml:"providers"`
	Log       LogConfig                 `yaml:"log"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	cfg := Config{Log: defaultLogConfig()}
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config: %w", err)
	}
	if err := Validate(&cfg); err != nil {
		return nil, err
	}
	return &cfg, nil
}
