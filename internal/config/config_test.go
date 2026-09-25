package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnvironmentOverridesConfigFile(t *testing.T) {
	configPath := filepath.Join(t.TempDir(), "config.toml")
	contents := []byte(`
[database]
host = "file-host"
port = 5432
user = "file-user"
password = "file-password"
name = "file-database"

[server]
readTimeout = "1s"
writeTimeout = "1s"
idleTimeout = "1s"

[redis]
refreshTokenTTL = "1h"
accessTokenTTL = "1h"

[jwt]
key = "file-jwt-key"
`)
	if err := os.WriteFile(configPath, contents, 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	t.Setenv("LOGIFLOW_DATABASE_HOST", "env-host")
	t.Setenv("LOGIFLOW_DATABASE_USER", "env-user")
	t.Setenv("LOGIFLOW_DATABASE_PASSWORD", "env-password")
	t.Setenv("LOGIFLOW_DATABASE_NAME", "env-database")
	t.Setenv("LOGIFLOW_JWT_KEY", "env-jwt-key")
	t.Setenv("LOGIFLOW_ROUTING_BASEURL", "http://osrm:8080")

	cfg, err := NewConfig(context.Background(), configPath)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Database.Host != "env-host" || cfg.Database.User != "env-user" || cfg.Database.Password != "env-password" || cfg.Database.Name != "env-database" {
		t.Fatalf("environment did not override database config: %+v", cfg.Database)
	}
	if cfg.JwtOpt.Key != "env-jwt-key" {
		t.Fatalf("environment did not override jwt key: %q", cfg.JwtOpt.Key)
	}
	if cfg.Routing.BaseURL != "http://osrm:8080" {
		t.Fatalf("environment did not configure routing URL: %q", cfg.Routing.BaseURL)
	}
}
