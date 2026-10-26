package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"dncs/backend/internal/auth"
	"dncs/backend/internal/authorization"
	"dncs/backend/internal/database"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	e2eMigrateOnce sync.Once
	e2eMigrateErr  error
)

func e2ePool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	databaseURL := os.Getenv("TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("TEST_DATABASE_URL is not set; HTTP/PostgreSQL E2E test skipped")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	e2eMigrateOnce.Do(func() {
		migrationDirectory, err := filepath.Abs("../../../database/migrations")
		if err != nil {
			e2eMigrateErr = err
			return
		}
		e2eMigrateErr = database.Migrate(ctx, pool, migrationDirectory)
	})
	if e2eMigrateErr != nil {
		t.Fatalf("migrate E2E database: %v", e2eMigrateErr)
	}
	return pool
}

func TestP6AuthenticationTraceAuditEndToEnd(t *testing.T) {
	pool := e2ePool(t)
	password := "P6-e2e-password-strong"
	passwordHash, err := auth.NewPasswordManager().Hash(password)
	if err != nil {
		t.Fatal(err)
	}
	username := fmt.Sprintf("p6.e2e.%d", time.Now().UnixNano())
	var userID int64
	if err = pool.QueryRow(context.Background(), `
		INSERT INTO users(role_id,account_type,username,normalized_username,password_hash,status)
		SELECT id,'STAFF',$1,$1,$2,'ACTIVE' FROM roles WHERE code='ADMIN'
		RETURNING id`, username, passwordHash).Scan(&userID); err != nil {
		t.Fatal(err)
	}
	if _, err = pool.Exec(context.Background(), `
		INSERT INTO staff_profiles(user_id,full_name,created_by,updated_by)
		VALUES($1,'P6 E2E Administrator',$1,$1)`, userID); err != nil {
		t.Fatal(err)
	}
	appConfig, err := pgxpool.ParseConfig(os.Getenv("TEST_DATABASE_URL"))
	if err != nil {
		t.Fatal(err)
	}
	appConfig.AfterConnect = func(ctx context.Context, connection *pgx.Conn) error {
		_, err := connection.Exec(ctx, `SET ROLE dncs_app`)
		return err
	}
	appPool, err := pgxpool.NewWithConfig(context.Background(), appConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer appPool.Close()
	if err = appPool.Ping(context.Background()); err != nil {
		t.Fatalf("application-role pool: %v", err)
	}

	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	tokens := auth.NewTokenManager("p6-e2e-jwt-secret-that-is-at-least-32-bytes", "dncs-p6-e2e", 15*time.Minute)
	store := auth.NewStore(appPool)
	service, err := auth.NewService(store, tokens, auth.NewPasswordManager(), auth.ServiceConfig{
		StaffRefreshTTL: 8 * time.Hour, PatientRefreshTTL: 30 * 24 * time.Hour,
		MaxLoginFailures: 5, LoginLockDuration: 15 * time.Minute,
	})
	if err != nil {
		t.Fatal(err)
	}
	e2eOptions := testOptions()
	e2eOptions.RequestTimeout = 5 * time.Second
	e2eOptions.AuthRateLimitMax = 10
	router := NewRouter(
		appPool,
		auth.NewHandler(service, logger),
		auth.NewAuthenticator(tokens, store, logger),
		authorization.NewStore(appPool),
		logger,
		e2eOptions,
	)
	server := httptest.NewServer(router)
	defer server.Close()

	loginRequestID := "550e8400-e29b-41d4-a716-446655440001"
	loginBody, _ := json.Marshal(map[string]string{"username": username, "password": password})
	loginRequest, err := http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/staff/login", bytes.NewReader(loginBody))
	if err != nil {
		t.Fatal(err)
	}
	loginRequest.Header.Set("Content-Type", "application/json")
	loginRequest.Header.Set(requestIDHeader, loginRequestID)
	loginResponse, err := server.Client().Do(loginRequest)
	if err != nil {
		t.Fatal(err)
	}
	defer loginResponse.Body.Close()
	var pair auth.TokenPair
	if err = json.NewDecoder(loginResponse.Body).Decode(&pair); err != nil {
		t.Fatal(err)
	}
	if loginResponse.StatusCode != http.StatusOK || pair.AccessToken == "" || loginResponse.Header.Get(requestIDHeader) != loginRequestID {
		t.Fatalf("login status=%d trace=%q token=%v", loginResponse.StatusCode, loginResponse.Header.Get(requestIDHeader), pair.AccessToken != "")
	}

	meRequest, _ := http.NewRequest(http.MethodGet, server.URL+"/api/v1/me", nil)
	meRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	meResponse, err := server.Client().Do(meRequest)
	if err != nil {
		t.Fatal(err)
	}
	meBody, _ := io.ReadAll(meResponse.Body)
	_ = meResponse.Body.Close()
	if meResponse.StatusCode != http.StatusOK || !bytes.Contains(meBody, []byte(`"role_code":"ADMIN"`)) {
		t.Fatalf("me status=%d body=%s", meResponse.StatusCode, meBody)
	}

	auditRequestID := "550e8400-e29b-41d4-a716-446655440002"
	auditCorrelationID := "6ba7b810-9dad-11d1-80b4-00c04fd430c8"
	logoutRequest, _ := http.NewRequest(http.MethodPost, server.URL+"/api/v1/auth/logout-all", nil)
	logoutRequest.Header.Set("Authorization", "Bearer "+pair.AccessToken)
	logoutRequest.Header.Set(requestIDHeader, auditRequestID)
	logoutRequest.Header.Set(correlationIDHeader, auditCorrelationID)
	logoutResponse, err := server.Client().Do(logoutRequest)
	if err != nil {
		t.Fatal(err)
	}
	_ = logoutResponse.Body.Close()
	if logoutResponse.StatusCode != http.StatusNoContent {
		t.Fatalf("logout-all status=%d", logoutResponse.StatusCode)
	}
	var storedRequestID, storedCorrelationID, ipHash string
	if err = pool.QueryRow(context.Background(), `
		SELECT request_id::text,correlation_id::text,ip_hash
		FROM audit_logs
		WHERE actor_user_id=$1 AND action_code='LOGOUT_ALL'
		ORDER BY id DESC LIMIT 1`, userID).Scan(&storedRequestID, &storedCorrelationID, &ipHash); err != nil {
		t.Fatal(err)
	}
	if storedRequestID != auditRequestID || storedCorrelationID != auditCorrelationID || ipHash == "" {
		t.Fatalf("audit trace request=%q correlation=%q ip_hash=%q", storedRequestID, storedCorrelationID, ipHash)
	}
	for _, secret := range []string{password, pair.AccessToken, pair.RefreshToken} {
		if strings.Contains(logs.String(), secret) {
			t.Fatalf("E2E log leaked a credential: %s", logs.String())
		}
	}
}
