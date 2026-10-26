package auth

import (
	"strings"
	"testing"
)

func TestNormalizeStaffUsername(t *testing.T) {
	normalized, err := NormalizeStaffUsername("  Admin.User  ")
	if err != nil || normalized != "admin.user" {
		t.Fatalf("NormalizeStaffUsername() = %q, %v", normalized, err)
	}
	for _, invalid := range []string{"ab", "bad user", "admin@dncs"} {
		if _, err := NormalizeStaffUsername(invalid); err == nil {
			t.Fatalf("NormalizeStaffUsername(%q) should fail", invalid)
		}
	}
}

func TestNormalizeVietnamPhone(t *testing.T) {
	tests := map[string]string{
		"0912 345 678":  "+84912345678",
		"+84-912345678": "+84912345678",
		"84912345678":   "+84912345678",
	}
	for input, expected := range tests {
		actual, err := NormalizeVietnamPhone(input)
		if err != nil || actual != expected {
			t.Errorf("NormalizeVietnamPhone(%q) = %q, %v; want %q", input, actual, err, expected)
		}
	}
	for _, invalid := range []string{"912345678", "+1 555 1234", "09abc123"} {
		if _, err := NormalizeVietnamPhone(invalid); err == nil {
			t.Errorf("NormalizeVietnamPhone(%q) should fail", invalid)
		}
	}
}

func TestPasswordManager(t *testing.T) {
	manager := NewPasswordManager()
	hash, err := manager.Hash("a-valid-password")
	if err != nil {
		t.Fatal(err)
	}
	if hash == "a-valid-password" || !manager.Matches(hash, "a-valid-password") {
		t.Fatal("password hash does not verify")
	}
	if manager.Matches(hash, "wrong-password") {
		t.Fatal("password hash accepted a wrong password")
	}
	if _, err := manager.Hash("short"); err == nil {
		t.Fatal("short password was accepted")
	}
	longUnicodePassword := "mật-khẩu-" + strings.Repeat("đ", 80)
	if _, err := manager.Hash(longUnicodePassword); err != nil {
		t.Fatalf("password within the 128-rune policy was rejected: %v", err)
	}
}
