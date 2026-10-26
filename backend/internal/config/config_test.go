package config

import (
	"strings"
	"testing"
	"time"
)

func setRequiredEnvironment(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://example.invalid/dncs")
	t.Setenv("JWT_SECRET", "test-jwt-secret-that-is-at-least-32-bytes")
	t.Setenv("AUDIT_IP_HASH_KEY", "test-audit-hash-key-that-is-at-least-32-bytes")
	t.Setenv("CORS_ALLOWED_ORIGINS", "")
}

func TestLoadCORSAllowlist(t *testing.T) {
	setRequiredEnvironment(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://127.0.0.1:5173, https://frontend.example.test")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := strings.Join(cfg.CORSAllowedOrigins, ","), "http://127.0.0.1:5173,https://frontend.example.test"; got != want {
		t.Fatalf("CORS allowed origins=%q want %q", got, want)
	}
	for _, value := range []string{"*", "http://localhost:5173/path", "ftp://example.test"} {
		t.Setenv("CORS_ALLOWED_ORIGINS", value)
		if _, err := Load(); err == nil || !strings.Contains(err.Error(), "CORS_ALLOWED_ORIGINS") {
			t.Fatalf("CORS value %q error=%v", value, err)
		}
	}
}

func TestLoadHardeningDefaults(t *testing.T) {
	setRequiredEnvironment(t)
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.RequestTimeout != 25*time.Second || cfg.WriteTimeout != 30*time.Second {
		t.Fatalf("timeout defaults request=%s write=%s", cfg.RequestTimeout, cfg.WriteTimeout)
	}
	if cfg.AuthRateLimitMax != 20 || cfg.AuthRateWindow != time.Minute {
		t.Fatalf("rate defaults max=%d window=%s", cfg.AuthRateLimitMax, cfg.AuthRateWindow)
	}
}

func TestLoadRejectsUnsafeHardeningConfig(t *testing.T) {
	t.Run("short audit key", func(t *testing.T) {
		setRequiredEnvironment(t)
		t.Setenv("AUDIT_IP_HASH_KEY", "short")
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "AUDIT_IP_HASH_KEY") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("request exceeds server write timeout", func(t *testing.T) {
		setRequiredEnvironment(t)
		t.Setenv("REQUEST_TIMEOUT", "30s")
		t.Setenv("HTTP_WRITE_TIMEOUT", "30s")
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "REQUEST_TIMEOUT") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
	t.Run("disabled rate limit", func(t *testing.T) {
		setRequiredEnvironment(t)
		t.Setenv("AUTH_RATE_LIMIT_MAX", "0")
		_, err := Load()
		if err == nil || !strings.Contains(err.Error(), "rate-limit") {
			t.Fatalf("unexpected error: %v", err)
		}
	})
}
