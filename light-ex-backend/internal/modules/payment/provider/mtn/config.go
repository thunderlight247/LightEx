package mtn

import (
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

const (
	DefaultBaseURL     = "https://sandbox.momodeveloper.mtn.com"
	DefaultEnvironment = "sandbox"
	DefaultTimeout     = 30 * time.Second
)

// SubscriptionConfig holds API credentials for one MTN product (Collection or Disbursement).
type SubscriptionConfig struct {
	SubscriptionKey string
	APIUser         string
	APIKey          string
}

// IsConfigured returns true if all three credentials are non-empty.
func (s SubscriptionConfig) IsConfigured() bool {
	return strings.TrimSpace(s.SubscriptionKey) != "" &&
		strings.TrimSpace(s.APIUser) != "" &&
		strings.TrimSpace(s.APIKey) != ""
}

// Config holds settings for the MTN MoMo provider client.
type Config struct {
	BaseURL      string
	Environment  string
	Collection   SubscriptionConfig
	Disbursement SubscriptionConfig
	CallbackURL  string
	Timeout      time.Duration
	HTTPClient   *http.Client
}

// LoadConfigFromEnv reads MTN configuration from environment variables and optionally .env.mtn.
func LoadConfigFromEnv() Config {
	// Attempt loading from .env.mtn if present
	_ = godotenv.Load(".env.mtn")

	baseURL := os.Getenv("MTN_BASE_URL")
	if strings.TrimSpace(baseURL) == "" {
		baseURL = DefaultBaseURL
	}

	env := os.Getenv("MTN_ENVIRONMENT")
	if strings.TrimSpace(env) == "" {
		env = DefaultEnvironment
	}

	return Config{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Environment: env,
		Collection: SubscriptionConfig{
			SubscriptionKey: os.Getenv("MTN_COLLECTION_SUBSCRIPTION_KEY"),
			APIUser:         os.Getenv("MTN_COLLECTION_API_USER"),
			APIKey:          os.Getenv("MTN_COLLECTION_API_KEY"),
		},
		Disbursement: SubscriptionConfig{
			SubscriptionKey: os.Getenv("MTN_DISBURSEMENT_SUBSCRIPTION_KEY"),
			APIUser:         os.Getenv("MTN_DISBURSEMENT_API_USER"),
			APIKey:          os.Getenv("MTN_DISBURSEMENT_API_KEY"),
		},
		CallbackURL: os.Getenv("MTN_CALLBACK_URL"),
		Timeout:     DefaultTimeout,
	}
}
