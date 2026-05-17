package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"

	"repos.apps.bluvalt.com/BluvaltCloud/netq-exporter/internal/netq"
)

const (
	defaultListenAddress = ":8080"
	defaultPollInterval  = time.Minute
	defaultTimeout       = 15 * time.Second

	envListenAddress          = "LISTEN_ADDRESS"
	envPollInterval           = "POLL_INTERVAL"
	envNetQHost               = "NETQ_HOST"
	envNetQUsername           = "NETQ_USERNAME"
	envNetQPassword           = "NETQ_PASSWORD"
	envNetQTimeout            = "NETQ_TIMEOUT"
	envNetQInsecureSkipVerify = "NETQ_INSECURE_SKIP_VERIFY"
)

type Config struct {
	ListenAddress string
	PollInterval  time.Duration
	NetQ          netq.Config
}

// Load reads exporter configuration from environment variables, applies
// defaults, and validates the resulting settings.
func Load() (Config, error) {
	pollInterval, err := getDuration(envPollInterval, defaultPollInterval)
	if err != nil {
		return Config{}, fmt.Errorf("parse POLL_INTERVAL: %w", err)
	}
	timeout, err := getDuration(envNetQTimeout, defaultTimeout)
	if err != nil {
		return Config{}, fmt.Errorf("parse NETQ_TIMEOUT: %w", err)
	}

	cfg := Config{
		ListenAddress: getEnv(envListenAddress, defaultListenAddress),
		PollInterval:  pollInterval,
		NetQ: netq.Config{
			Host:               os.Getenv(envNetQHost),
			Username:           os.Getenv(envNetQUsername),
			Password:           os.Getenv(envNetQPassword),
			InsecureSkipVerify: getBool(envNetQInsecureSkipVerify, true),
			Timeout:            timeout,
		},
	}

	if err := validate(cfg); err != nil {
		return Config{}, err
	}

	return cfg, nil
}

func validate(cfg Config) error {
	if cfg.NetQ.Host == "" || cfg.NetQ.Username == "" || cfg.NetQ.Password == "" {
		return errors.New("NETQ_HOST, NETQ_USERNAME, and NETQ_PASSWORD are required")
	}
	if cfg.PollInterval <= 0 {
		return errors.New("POLL_INTERVAL must be positive")
	}
	if cfg.NetQ.Timeout <= 0 {
		return errors.New("NETQ_TIMEOUT must be positive")
	}
	return nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func getDuration(key string, fallback time.Duration) (time.Duration, error) {
	v := os.Getenv(key)
	if v == "" {
		return fallback, nil
	}
	return time.ParseDuration(v)
}

func getBool(key string, fallback bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(v)
	if err != nil {
		return fallback
	}
	return parsed
}
