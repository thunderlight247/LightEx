package config

import (
	"strings"
	"testing"
)

func TestSimulatedLiquidityScenarioConfiguration(t *testing.T) {
	t.Setenv("APP_NAME", "LightEx")
	t.Setenv("APP_ENV", "development")
	t.Setenv("SERVER_PORT", "8080")
	t.Setenv("DB_HOST", "localhost")
	t.Setenv("DB_NAME", "lightex")
	t.Setenv("DB_USER", "test")
	t.Setenv("DB_PASSWORD", "test")

	for _, scenario := range []string{"", "success", "pending", "failure", "reject", "timeout"} {
		t.Run("scenario="+scenario, func(t *testing.T) {
			t.Setenv("SIMULATED_LIQUIDITY_SCENARIO", scenario)
			cfg, err := Load()
			if err != nil {
				t.Fatal(err)
			}
			want := scenario
			if want == "" {
				want = "success"
			}
			if cfg.App.SimulatedLiquidityScenario != want {
				t.Fatalf("scenario = %q, want %q", cfg.App.SimulatedLiquidityScenario, want)
			}
		})
	}
	t.Setenv("SIMULATED_LIQUIDITY_SCENARIO", "invalid")
	if _, err := Load(); err == nil || !strings.Contains(err.Error(), "SIMULATED_LIQUIDITY_SCENARIO") {
		t.Fatalf("expected scenario configuration error, got %v", err)
	}
	// The simulator is not mounted outside development.
	t.Setenv("APP_ENV", "production")
	if _, err := Load(); err != nil {
		t.Fatalf("unused simulation setting must not prevent production startup: %v", err)
	}
}
