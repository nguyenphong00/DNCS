package bootstrap

import (
	"context"
	"testing"

	"dncs/backend/internal/auth"
)

func TestSuperAdminRejectsInvalidInputBeforeDatabaseAccess(t *testing.T) {
	tests := []Input{
		{Username: "x", FullName: "Admin", Password: "valid-password"},
		{Username: "bootstrap.admin", FullName: " ", Password: "valid-password"},
	}
	for _, input := range tests {
		if _, err := SuperAdmin(context.Background(), nil, auth.NewPasswordManager(), input); err == nil {
			t.Fatalf("expected invalid input to fail: %+v", input)
		}
	}
}
