package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Download DownloadConfig `yaml:"download"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
	Mode string `yaml:"mode"`
}

type DatabaseConfig struct {
	Path string `yaml:"path"`
}

type AuthConfig struct {
	Secret          string `yaml:"secret"`
	TimestampWindow int64  `yaml:"timestamp_window"`
}

type DownloadConfig struct {
	RootDir string `yaml:"root_dir"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %q: %w", path, err)
	}
	var c Config
	if err := yaml.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	if c.Server.Addr == "" {
		c.Server.Addr = ":8080"
	}
	if c.Auth.TimestampWindow == 0 {
		c.Auth.TimestampWindow = 300
	}
	if c.Auth.Secret == "" || c.Auth.Secret == "CHANGE_ME_TO_A_LONG_RANDOM_STRING" {
		return nil, fmt.Errorf("auth.secret is not set — edit %s", path)
	}
	return &c, nil
}
