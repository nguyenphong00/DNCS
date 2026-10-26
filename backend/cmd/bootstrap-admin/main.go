package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"dncs/backend/internal/auth"
	"dncs/backend/internal/bootstrap"
	"dncs/backend/internal/database"
)

const maxPasswordBytes = 4096

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	databaseURL := strings.TrimSpace(os.Getenv("DATABASE_URL"))
	username := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_USERNAME"))
	fullName := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_FULL_NAME"))
	passwordFile := strings.TrimSpace(os.Getenv("BOOTSTRAP_ADMIN_PASSWORD_FILE"))
	if databaseURL == "" || username == "" || fullName == "" || passwordFile == "" {
		logger.Error("bootstrap configuration is incomplete", "required", "DATABASE_URL, BOOTSTRAP_ADMIN_USERNAME, BOOTSTRAP_ADMIN_FULL_NAME, BOOTSTRAP_ADMIN_PASSWORD_FILE")
		os.Exit(1)
	}
	password, err := readPassword(passwordFile)
	if err != nil {
		logger.Error("read bootstrap password", "error", err)
		os.Exit(1)
	}
	defer clear(password)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := database.Open(ctx, databaseURL)
	if err != nil {
		logger.Error("open database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	result, err := bootstrap.SuperAdmin(ctx, pool, auth.NewPasswordManager(), bootstrap.Input{
		Username: username, FullName: fullName, Password: string(password),
	})
	if err != nil {
		logger.Error("bootstrap super-admin", "error", err)
		os.Exit(1)
	}
	logger.Info("super-admin bootstrap complete", "created", result.Created, "user_id", result.UserID, "staff_profile_id", result.StaffProfileID, "username", result.Username)
}

func readPassword(path string) ([]byte, error) {
	var content []byte
	var err error
	if path == "-" {
		content, err = io.ReadAll(io.LimitReader(os.Stdin, maxPasswordBytes+1))
	} else {
		content, err = os.ReadFile(path)
	}
	if err != nil {
		return nil, err
	}
	if len(content) > maxPasswordBytes {
		return nil, errors.New("password secret is too large")
	}
	content = trimOneLineEnding(content)
	if len(content) == 0 {
		return nil, errors.New("password secret is empty")
	}
	return content, nil
}

func trimOneLineEnding(value []byte) []byte {
	if len(value) > 0 && value[len(value)-1] == '\n' {
		value = value[:len(value)-1]
		if len(value) > 0 && value[len(value)-1] == '\r' {
			value = value[:len(value)-1]
		}
	}
	return value
}

func clear(value []byte) {
	for index := range value {
		value[index] = 0
	}
}
