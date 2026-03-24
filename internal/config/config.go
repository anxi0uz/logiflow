package config

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/knadh/koanf/parsers/toml"
	"github.com/knadh/koanf/providers/env"
	"github.com/knadh/koanf/providers/file"
	"github.com/knadh/koanf/v2"
)

type Config struct {
	Logiflow struct {
		LogLevel slog.Level `koanf:"logLevel"`
	} `koanf:"logiflow"`

	Database struct {
		Host     string `koanf:"host"`
		Port     int    `koanf:"port"`
		User     string `koanf:"user"`
		Password string `koanf:"password"`
		Name     string `koanf:"name"`
		SslMode  string `koanf:"sslmode"`
		URL      string
	} `koanf:"database"`

	Server struct {
		Host            string `koanf:"host"`
		Port            int    `koanf:"port"`
		ReadTimeout     string `koanf:"readTimeout"`
		WriteTimeout    string `koanf:"writeTimeout"`
		IdleTimeout     string `koanf:"idleTimeout"`
		ReadTimeoutDur  time.Duration
		WriteTimeoutDur time.Duration
		IdleTimeoutDur  time.Duration
		URL             string
	} `koanf:"server"`

	Redis struct {
		Addr            string `koanf:"addr"`
		Password        string `koanf:"password"`
		DB              int    `koanf:"db"`
		RefreshTokenTTL string `koanf:"refreshTokenTTL"`
		AccessTokenTTL  string `koanf:"accessTokenTTL"`
		RefreshTokenDur time.Duration
		AccessTokenDur  time.Duration
	} `koanf:"redis"`

	JwtOpt struct {
		Key      string `koanf:"key"`
		Issuer   string `koanf:"issuer"`
		Audience string `koanf:"audience"`
	} `koanf:"jwt"`

	Pricing struct {
		BaseFee   float64 `koanf:"baseFee"`
		PerKm     float64 `koanf:"perKm"`
		PerKg     float64 `koanf:"perKg"`
		PerM3     float64 `koanf:"perM3"`
	} `koanf:"pricing"`
}

func NewConfig(ctx context.Context, configPath string) (*Config, error) {
	if err := godotenv.Load(); err != nil && !os.IsNotExist(err) {
		slog.WarnContext(ctx, "Не удалось загрузить .env (возможно, файла нет)", "error", err)
	}

	k := koanf.New(".")

	if err := k.Load(env.Provider("LOGIFLOW_", ".", func(s string) string {
		return strings.ReplaceAll(strings.ToLower(strings.TrimPrefix(s, "LOGIFLOW_")), "_", ".")
	}), nil); err != nil {
		return nil, fmt.Errorf("ошибка загрузки ENV: %w", err)
	}

	if err := k.Load(file.Provider(configPath), toml.Parser()); err != nil {
		if !os.IsNotExist(err) {
			return nil, fmt.Errorf("не удалось прочитать config.toml: %w", err)
		}
		slog.InfoContext(ctx, "config.toml не найден — используем только ENV и дефолты")
	}

	var cfg Config
	if err := k.Unmarshal("", &cfg); err != nil {
		return nil, fmt.Errorf("не удалось размапить конфигурацию: %w", err)
	}

	cfg.setDefaults()

	if err := cfg.parseDurations(); err != nil {
		return nil, fmt.Errorf("ошибка парсинга длительностей: %w", err)
	}

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("конфигурация невалидна: %w", err)
	}

	cfg.Server.URL = fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port)
	cfg.Database.URL = cfg.makePostgresURL()

	slog.InfoContext(ctx, "Конфигурация загружена успешно",
		slog.String("db_host", cfg.Database.Host),
		slog.String("db_user", cfg.Database.User),
		slog.String("db_pass", maskSecret(cfg.Database.Password)),
		slog.String("db_name", cfg.Database.Name),
		slog.String("redis_addr", cfg.Redis.Addr),
		slog.String("log_level", cfg.Logiflow.LogLevel.String()),
	)

	return &cfg, nil
}

func maskSecret(s string) string {
	if s == "" {
		return "<empty>"
	}
	if len(s) <= 4 {
		return "****"
	}
	return s[:2] + strings.Repeat("*", len(s)-4) + s[len(s)-2:]
}

func (c *Config) makePostgresURL() string {
	return fmt.Sprintf("postgres://%s:%s@%s:%d/%s?sslmode=%s",
		c.Database.User,
		c.Database.Password,
		c.Database.Host,
		c.Database.Port,
		c.Database.Name,
		c.Database.SslMode,
	)
}

func (c *Config) setDefaults() {
	if c.Logiflow.LogLevel == 0 {
		c.Logiflow.LogLevel = slog.LevelInfo
	}
	if c.Database.Host == "" {
		c.Database.Host = "localhost"
	}
	if c.Database.Port == 0 {
		c.Database.Port = 5432
	}
	if c.Database.SslMode == "" {
		c.Database.SslMode = "disable"
	}
	if c.Server.Host == "" {
		c.Server.Host = "0.0.0.0"
	}
	if c.Server.Port == 0 {
		c.Server.Port = 3001
	}
	if c.Redis.Addr == "" {
		c.Redis.Addr = "localhost:6379"
	}
	if c.Pricing.BaseFee == 0 {
		c.Pricing.BaseFee = 500.0
	}
	if c.Pricing.PerKm == 0 {
		c.Pricing.PerKm = 25.0
	}
	if c.Pricing.PerKg == 0 {
		c.Pricing.PerKg = 3.0
	}
	if c.Pricing.PerM3 == 0 {
		c.Pricing.PerM3 = 150.0
	}
}

func (c *Config) parseDurations() error {
	var err error

	parse := func(name, s string) (time.Duration, error) {
		d, e := time.ParseDuration(s)
		if e != nil {
			return 0, fmt.Errorf("%s %q: %w", name, s, e)
		}
		return d, nil
	}

	c.Server.ReadTimeoutDur, err = parse("readTimeout", c.Server.ReadTimeout)
	if err != nil {
		return err
	}
	c.Server.WriteTimeoutDur, err = parse("writeTimeout", c.Server.WriteTimeout)
	if err != nil {
		return err
	}
	c.Server.IdleTimeoutDur, err = parse("idleTimeout", c.Server.IdleTimeout)
	if err != nil {
		return err
	}
	c.Redis.RefreshTokenDur, err = parse("refreshTokenTTL", c.Redis.RefreshTokenTTL)
	if err != nil {
		return err
	}
	c.Redis.AccessTokenDur, err = parse("accessTokenTTL", c.Redis.AccessTokenTTL)
	if err != nil {
		return err
	}

	return nil
}

func (c *Config) validate() error {
	if c.Database.Host == "" {
		return fmt.Errorf("database.host обязателен")
	}
	if c.Database.User == "" {
		return fmt.Errorf("database.user обязателен")
	}
	if c.Database.Password == "" {
		return fmt.Errorf("database.password обязателен")
	}
	if c.Database.Name == "" {
		return fmt.Errorf("database.name обязателен")
	}
	if c.JwtOpt.Key == "" {
		return fmt.Errorf("jwt.key обязателен")
	}
	return nil
}

func (c *Config) DatabaseURL() string                 { return c.Database.URL }
func (c *Config) ServerURL() string                   { return c.Server.URL }
func (c *Config) ReadTimeout() time.Duration          { return c.Server.ReadTimeoutDur }
func (c *Config) WriteTimeout() time.Duration         { return c.Server.WriteTimeoutDur }
func (c *Config) IdleTimeout() time.Duration          { return c.Server.IdleTimeoutDur }
func (c *Config) RedisAccessTokenDur() time.Duration  { return c.Redis.AccessTokenDur }
func (c *Config) RedisRefreshTokenDur() time.Duration { return c.Redis.RefreshTokenDur }
