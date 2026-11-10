package config

import "errors"

// validate checks whether the loaded configuration is valid.
// The application must not start if validation fails.
func validate(cfg *Config) error {

	if cfg == nil {
		return errors.New("configuration cannot be nil")
	}

	// Application
	if cfg.App.Name == "" {
		return errors.New("APP_NAME is required")
	}

	if cfg.App.Env == "development" {
		switch cfg.App.SimulatedLiquidityScenario {
		case "", "success", "pending", "failure", "reject", "timeout":
		default:
			return errors.New("SIMULATED_LIQUIDITY_SCENARIO must be success, pending, failure, reject, or timeout")
		}
	}

	// Server
	if cfg.Server.Port <= 0 {
		return errors.New("SERVER_PORT must be greater than 0")
	}

	// Database
	if cfg.Database.Host == "" {
		return errors.New("DB_HOST is required")
	}

	if cfg.Database.Name == "" {
		return errors.New("DB_NAME is required")
	}

	if cfg.Database.User == "" {
		return errors.New("DB_USER is required")
	}

	if cfg.Database.Password == "" {
		return errors.New("DB_PASSWORD is required")
	}

	return nil
}
