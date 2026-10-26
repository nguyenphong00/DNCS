package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr           string
	DatabaseURL        string
	JWTSecret          string
	JWTIssuer          string
	AccessTokenTTL     time.Duration
	StaffRefreshTTL    time.Duration
	PatientRefreshTTL  time.Duration
	LoginMaxFailures   int
	LoginLockDuration  time.Duration
	RequestTimeout     time.Duration
	ReadHeaderTimeout  time.Duration
	ReadTimeout        time.Duration
	WriteTimeout       time.Duration
	IdleTimeout        time.Duration
	AuthRateLimitMax   int
	AuthRateWindow     time.Duration
	AuditIPHashKey     string
	CORSAllowedOrigins []string
	MigrationsDir      string
	MigrateOnStart     bool
}

func Load() (Config, error) {
	cfg := Config{
		HTTPAddr:           envOr("HTTP_ADDR", ":8080"),
		DatabaseURL:        strings.TrimSpace(os.Getenv("DATABASE_URL")),
		JWTSecret:          os.Getenv("JWT_SECRET"),
		JWTIssuer:          envOr("JWT_ISSUER", "dncs-api"),
		MigrationsDir:      envOr("MIGRATIONS_DIR", "../database/migrations"),
		AccessTokenTTL:     15 * time.Minute,
		StaffRefreshTTL:    8 * time.Hour,
		PatientRefreshTTL:  30 * 24 * time.Hour,
		LoginMaxFailures:   5,
		LoginLockDuration:  15 * time.Minute,
		RequestTimeout:     25 * time.Second,
		ReadHeaderTimeout:  5 * time.Second,
		ReadTimeout:        15 * time.Second,
		WriteTimeout:       30 * time.Second,
		IdleTimeout:        60 * time.Second,
		AuthRateLimitMax:   20,
		AuthRateWindow:     time.Minute,
		AuditIPHashKey:     os.Getenv("AUDIT_IP_HASH_KEY"),
		CORSAllowedOrigins: nil,
	}

	var err error
	if cfg.AccessTokenTTL, err = durationEnv("ACCESS_TOKEN_TTL", cfg.AccessTokenTTL); err != nil {
		return Config{}, err
	}
	if cfg.StaffRefreshTTL, err = durationEnv("STAFF_REFRESH_TTL", cfg.StaffRefreshTTL); err != nil {
		return Config{}, err
	}
	if cfg.PatientRefreshTTL, err = durationEnv("PATIENT_REFRESH_TTL", cfg.PatientRefreshTTL); err != nil {
		return Config{}, err
	}
	if cfg.LoginLockDuration, err = durationEnv("LOGIN_LOCK_DURATION", cfg.LoginLockDuration); err != nil {
		return Config{}, err
	}
	for key, target := range map[string]*time.Duration{
		"REQUEST_TIMEOUT":          &cfg.RequestTimeout,
		"HTTP_READ_HEADER_TIMEOUT": &cfg.ReadHeaderTimeout,
		"HTTP_READ_TIMEOUT":        &cfg.ReadTimeout,
		"HTTP_WRITE_TIMEOUT":       &cfg.WriteTimeout,
		"HTTP_IDLE_TIMEOUT":        &cfg.IdleTimeout,
		"AUTH_RATE_LIMIT_WINDOW":   &cfg.AuthRateWindow,
	} {
		if *target, err = durationEnv(key, *target); err != nil {
			return Config{}, err
		}
	}
	if cfg.LoginMaxFailures, err = intEnv("LOGIN_MAX_FAILURES", cfg.LoginMaxFailures); err != nil {
		return Config{}, err
	}
	if cfg.AuthRateLimitMax, err = intEnv("AUTH_RATE_LIMIT_MAX", cfg.AuthRateLimitMax); err != nil {
		return Config{}, err
	}
	if cfg.MigrateOnStart, err = boolEnv("APP_MIGRATE_ON_START", false); err != nil {
		return Config{}, err
	}
	if cfg.CORSAllowedOrigins, err = originsEnv("CORS_ALLOWED_ORIGINS"); err != nil {
		return Config{}, err
	}

	if cfg.DatabaseURL == "" {
		return Config{}, errors.New("DATABASE_URL is required")
	}
	if len(cfg.JWTSecret) < 32 {
		return Config{}, errors.New("JWT_SECRET must contain at least 32 bytes")
	}
	if cfg.JWTIssuer == "" {
		return Config{}, errors.New("JWT_ISSUER must not be empty")
	}
	if cfg.AccessTokenTTL <= 0 || cfg.StaffRefreshTTL <= 0 || cfg.PatientRefreshTTL <= 0 {
		return Config{}, errors.New("token TTL values must be positive")
	}
	if cfg.LoginLockDuration <= 0 {
		return Config{}, errors.New("LOGIN_LOCK_DURATION must be positive")
	}
	if cfg.LoginMaxFailures < 1 {
		return Config{}, errors.New("LOGIN_MAX_FAILURES must be at least 1")
	}
	if cfg.RequestTimeout <= 0 || cfg.ReadHeaderTimeout <= 0 || cfg.ReadTimeout <= 0 || cfg.WriteTimeout <= 0 || cfg.IdleTimeout <= 0 {
		return Config{}, errors.New("HTTP timeout values must be positive")
	}
	if cfg.RequestTimeout >= cfg.WriteTimeout {
		return Config{}, errors.New("REQUEST_TIMEOUT must be shorter than HTTP_WRITE_TIMEOUT")
	}
	if cfg.AuthRateLimitMax < 1 || cfg.AuthRateWindow <= 0 {
		return Config{}, errors.New("authentication rate-limit values must be positive")
	}
	if len(cfg.AuditIPHashKey) < 32 {
		return Config{}, errors.New("AUDIT_IP_HASH_KEY must contain at least 32 bytes")
	}
	if strings.TrimSpace(cfg.MigrationsDir) == "" {
		return Config{}, errors.New("MIGRATIONS_DIR must not be empty")
	}
	return cfg, nil
}

func originsEnv(key string) ([]string, error) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return nil, nil
	}
	seen := make(map[string]struct{})
	origins := make([]string, 0)
	for _, item := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(item)
		if origin == "" || origin == "*" {
			return nil, fmt.Errorf("%s must contain explicit http(s) origins", key)
		}
		parsed, err := url.ParseRequestURI(origin)
		if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.User != nil {
			return nil, fmt.Errorf("%s contains invalid origin %q", key, origin)
		}
		if _, exists := seen[origin]; exists {
			continue
		}
		seen[origin] = struct{}{}
		origins = append(origins, origin)
	}
	return origins, nil
}

func envOr(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return duration, nil
}

func intEnv(key string, fallback int) (int, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}

func boolEnv(key string, fallback bool) (bool, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return false, fmt.Errorf("%s: %w", key, err)
	}
	return parsed, nil
}
