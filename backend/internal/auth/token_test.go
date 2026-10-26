package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTokenManagerIssueAndVerify(t *testing.T) {
	fixedNow := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	manager := NewTokenManager(strings.Repeat("s", 32), "dncs-test", 15*time.Minute)
	manager.now = func() time.Time { return fixedNow }

	token, expiresAt, err := manager.IssueAccess(42, 84, 3)
	if err != nil {
		t.Fatalf("IssueAccess() error = %v", err)
	}
	if want := fixedNow.Add(15 * time.Minute); !expiresAt.Equal(want) {
		t.Fatalf("expiresAt = %v, want %v", expiresAt, want)
	}
	claims, err := manager.VerifyAccess(token)
	if err != nil {
		t.Fatalf("VerifyAccess() error = %v", err)
	}
	if claims.Subject != "42" || claims.SessionID != "84" || claims.TokenVersion != 3 {
		t.Fatalf("unexpected claims: %+v", claims)
	}
}

func TestTokenManagerRejectsExpiredAndTamperedTokens(t *testing.T) {
	fixedNow := time.Date(2026, 9, 1, 10, 0, 0, 0, time.UTC)
	manager := NewTokenManager(strings.Repeat("s", 32), "dncs-test", time.Minute)
	manager.now = func() time.Time { return fixedNow }
	token, _, err := manager.IssueAccess(1, 2, 1)
	if err != nil {
		t.Fatal(err)
	}

	manager.now = func() time.Time { return fixedNow.Add(2 * time.Minute) }
	if _, err := manager.VerifyAccess(token); err == nil {
		t.Fatal("VerifyAccess() accepted an expired token")
	}

	manager.now = func() time.Time { return fixedNow }
	tampered := token[:len(token)-1] + "A"
	if _, err := manager.VerifyAccess(tampered); err == nil {
		t.Fatal("VerifyAccess() accepted a tampered token")
	}
}

func TestRefreshTokenGeneration(t *testing.T) {
	rawOne, hashOne, err := GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	rawTwo, hashTwo, err := GenerateRefreshToken()
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateRefreshToken(rawOne); err != nil {
		t.Fatalf("ValidateRefreshToken() error = %v", err)
	}
	if rawOne == rawTwo || hashOne == hashTwo {
		t.Fatal("refresh token generator returned a duplicate")
	}
	if rawOne == hashOne || HashRefreshToken(rawOne) != hashOne {
		t.Fatal("refresh token was not hashed as expected")
	}
	if err := ValidateRefreshToken("not-a-token"); err == nil {
		t.Fatal("ValidateRefreshToken() accepted malformed input")
	}
}
