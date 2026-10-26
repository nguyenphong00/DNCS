package database_test

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"dncs/backend/internal/auth"
)

func TestAccountAdministrationResetLockUnlockAndDeactivate(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	passwords := auth.NewPasswordManager()
	service, err := auth.NewService(
		auth.NewStore(f.pool),
		auth.NewTokenManager(strings.Repeat("a", 32), "dncs-account-admin-test", 15*time.Minute),
		passwords,
		auth.ServiceConfig{},
	)
	if err != nil {
		t.Fatal(err)
	}
	targetUserID := f.doctor.UserID
	var version int64
	if err := f.pool.QueryRow(ctx, `SELECT version FROM users WHERE id=$1`, targetUserID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, err := f.pool.Exec(ctx, `
		INSERT INTO auth_sessions(user_id,refresh_token_hash,issued_at,expires_at)
		VALUES($1,$2,now(),now()+interval '1 hour')`, targetUserID, fmt.Sprintf("account-admin-session-%d", time.Now().UnixNano())); err != nil {
		t.Fatal(err)
	}

	reset, err := service.ResetPassword(ctx, f.admin, targetUserID, auth.AccountAdminInput{ExpectedVersion: version, Reason: "Khôi phục truy cập tại quầy"})
	if err != nil {
		t.Fatalf("reset password: %v", err)
	}
	if reset.TemporaryPassword == "" || !reset.MustChangePassword || reset.Version != version+1 {
		t.Fatalf("unexpected reset result: %+v", reset)
	}
	var hash string
	var activeSessions int
	if err := f.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id=$1`, targetUserID).Scan(&hash); err != nil {
		t.Fatal(err)
	}
	if !passwords.Matches(hash, reset.TemporaryPassword) {
		t.Fatal("temporary password does not match stored Argon2id hash")
	}
	if err := f.pool.QueryRow(ctx, `SELECT count(*) FROM auth_sessions WHERE user_id=$1 AND revoked_at IS NULL`, targetUserID).Scan(&activeSessions); err != nil || activeSessions != 0 {
		t.Fatalf("active sessions after reset=%d err=%v", activeSessions, err)
	}

	locked, err := service.LockAccount(ctx, f.admin, targetUserID, auth.AccountAdminInput{ExpectedVersion: reset.Version, Reason: "Điều tra bảo mật"})
	if err != nil || locked.Status != "LOCKED" {
		t.Fatalf("lock result=%+v err=%v", locked, err)
	}
	unlocked, err := service.UnlockAccount(ctx, f.admin, targetUserID, auth.AccountAdminInput{ExpectedVersion: locked.Version, Reason: "Đã xác minh danh tính"})
	if err != nil || unlocked.Status != "ACTIVE" {
		t.Fatalf("unlock result=%+v err=%v", unlocked, err)
	}
	deactivated, err := service.DeactivateAccount(ctx, f.admin, targetUserID, auth.AccountAdminInput{ExpectedVersion: unlocked.Version, Reason: "Ngừng truy cập độc lập"})
	if err != nil || deactivated.Status != "INACTIVE" {
		t.Fatalf("deactivate result=%+v err=%v", deactivated, err)
	}

	var auditCount int
	if err := f.pool.QueryRow(ctx, `
		SELECT count(*) FROM audit_logs WHERE target_type='USER' AND target_id=$1
		AND action_code IN ('ACCOUNT_PASSWORD_RESET','ACCOUNT_LOCKED','ACCOUNT_UNLOCKED','ACCOUNT_DEACTIVATED')`, targetUserID).Scan(&auditCount); err != nil {
		t.Fatal(err)
	}
	if auditCount != 4 {
		t.Fatalf("account administration audit count=%d want 4", auditCount)
	}
	_, err = service.UnlockAccount(ctx, f.admin, targetUserID, auth.AccountAdminInput{ExpectedVersion: unlocked.Version, Reason: "stale"})
	if err != auth.ErrAccountConcurrentUpdate {
		t.Fatalf("stale expected_version error=%v", err)
	}
}

func TestAccountAdministrationProtectsSelfAndSuperAdmin(t *testing.T) {
	f := newFixture(t)
	service, err := auth.NewService(
		auth.NewStore(f.pool),
		auth.NewTokenManager(strings.Repeat("b", 32), "dncs-account-admin-test", 15*time.Minute),
		auth.NewPasswordManager(),
		auth.ServiceConfig{},
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.LockAccount(context.Background(), f.admin, f.admin.UserID, auth.AccountAdminInput{ExpectedVersion: 1, Reason: "self"}); err != auth.ErrAccountSelfAdmin {
		t.Fatalf("self administration error=%v", err)
	}
	superUserID := insertStaffUser(t, f.pool, roleID(t, f.pool, "SUPER_ADMIN"), fmt.Sprintf("protected_%d", nextFixtureID(t, f.pool)))
	// Keep the shared integration database hermetic: bootstrap tests require that no
	// SUPER_ADMIN exists, so demote this fixture once the protection assertion ran.
	t.Cleanup(func() {
		_, _ = f.pool.Exec(context.Background(), `UPDATE users SET role_id=$2 WHERE id=$1`, superUserID, roleID(t, f.pool, "ADMIN"))
	})
	var version int64
	if err := f.pool.QueryRow(context.Background(), `SELECT version FROM users WHERE id=$1`, superUserID).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ResetPassword(context.Background(), f.admin, superUserID, auth.AccountAdminInput{ExpectedVersion: version, Reason: "forbidden"}); err != auth.ErrAccountProtected {
		t.Fatalf("protected account error=%v", err)
	}
}
