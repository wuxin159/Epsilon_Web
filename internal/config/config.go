package config

import (
	"fmt"
	"net/url"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server   ServerConfig   `yaml:"server"`
	Database DatabaseConfig `yaml:"database"`
	Auth     AuthConfig     `yaml:"auth"`
	Download DownloadConfig `yaml:"download"`
	Admin    AdminConfig    `yaml:"admin"`
}

type AdminConfig struct {
	Username string `yaml:"username"`
	Password string `yaml:"password"`
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
	BaseURL string `yaml:"base_url"`
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
	if c.Download.BaseURL != "" {
		u, err := url.Parse(c.Download.BaseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			return nil, fmt.Errorf("download.base_url must be an http(s) URL without credentials, query or fragment")
		}
		c.Download.BaseURL = strings.TrimRight(u.String(), "/")
	}
	return &c, nil
}
