// Package config provides application configuration loading.
package config

import (
	"fmt"
	"time"

	"github.com/joho/godotenv"
)

// Config represents the complete application configuration.
type Config struct {
	App      AppConfig
	Server   ServerConfig
	Database DatabaseConfig
	JWT      JWTConfig
}

// AppConfig contains application settings.
type AppConfig struct {
	Name                       string
	Env                        string
	SimulatedLiquidityScenario string
}

// ServerConfig contains HTTP server settings.
type ServerConfig struct {
	Host string
	Port int
}

// DatabaseConfig contains PostgreSQL configuration.
type DatabaseConfig struct {
	Host     string
	Port     int
	Name     string
	User     string
	Password string
	SSLMode  string

	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// JWTConfig contains JWT settings.
type JWTConfig struct {
	Secret          string
	Issuer          string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
}

// Load loads the application configuration.
func Load() (*Config, error) {

	_ = godotenv.Load()

	cfg := &Config{
		App: AppConfig{
			Name:                       getEnv("APP_NAME", "LightEx"),
			Env:                        getEnv("APP_ENV", "development"),
			SimulatedLiquidityScenario: getEnv("SIMULATED_LIQUIDITY_SCENARIO", "success"),
		},

		Server: ServerConfig{
			Host: getEnv("SERVER_HOST", "0.0.0.0"),
			Port: getEnvAsInt("SERVER_PORT", 8080),
		},

		Database: DatabaseConfig{
			Host:            getEnv("DB_HOST", "localhost"),
			Port:            getEnvAsInt("DB_PORT", 5432),
			Name:            getEnv("DB_NAME", "lightex"),
			User:            getEnv("DB_USER", "lightex_app"),
			Password:        getEnv("DB_PASSWORD", ""),
			SSLMode:         getEnv("DB_SSLMODE", "disable"),
			MaxOpenConns:    getEnvAsInt("DB_MAX_OPEN_CONNS", 25),
			MaxIdleConns:    getEnvAsInt("DB_MAX_IDLE_CONNS", 10),
			ConnMaxLifetime: getEnvAsDuration("DB_CONN_MAX_LIFETIME", 30*time.Minute),
			ConnMaxIdleTime: getEnvAsDuration("DB_CONN_MAX_IDLE_TIME", 15*time.Minute),
		},

		JWT: JWTConfig{
			Secret: getEnv(
				"JWT_SECRET",
				"CHANGE_THIS_IN_PRODUCTION",
			),

			Issuer: getEnv(
				"JWT_ISSUER",
				"LightEx",
			),

			AccessTokenTTL: getEnvAsDuration(
				"JWT_ACCESS_TTL",
				15*time.Minute,
			),

			RefreshTokenTTL: getEnvAsDuration(
				"JWT_REFRESH_TTL",
				7*24*time.Hour,
			),
		},
	}

	if err := validate(cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// DSN returns the PostgreSQL connection string.
func (d DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		d.Host,
		d.Port,
		d.User,
		d.Password,
		d.Name,
		d.SSLMode,
	)
}

// SafeDSN returns the PostgreSQL connection string with the password masked.
func (d DatabaseConfig) SafeDSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=****** dbname=%s sslmode=%s",
		d.Host,
		d.Port,
		d.User,
		d.Name,
		d.SSLMode,
	)
}

// MigrationURL returns the PostgreSQL URL used by golang-migrate.
func (d DatabaseConfig) MigrationURL() string {
	return fmt.Sprintf(
		"postgres://%s:%s@%s:%d/%s?sslmode=%s",
		d.User,
		d.Password,
		d.Host,
		d.Port,
		d.Name,
		d.SSLMode,
	)
}
