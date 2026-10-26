package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

const (
	argonMemoryKiB   uint32 = 64 * 1024
	argonIterations  uint32 = 3
	argonParallelism uint8  = 2
	argonSaltLength         = 16
	argonKeyLength          = 32
)

type PasswordManager struct{}

func NewPasswordManager() PasswordManager { return PasswordManager{} }

func (PasswordManager) Hash(password string) (string, error) {
	if !validPasswordLength(password) {
		return "", ErrPasswordPolicy
	}
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemoryKiB, argonParallelism, argonKeyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version, argonMemoryKiB, argonIterations, argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key),
	), nil
}

func (PasswordManager) Matches(encodedHash, password string) bool {
	if strings.HasPrefix(encodedHash, "$2a$") || strings.HasPrefix(encodedHash, "$2b$") || strings.HasPrefix(encodedHash, "$2y$") {
		return bcrypt.CompareHashAndPassword([]byte(encodedHash), []byte(password)) == nil
	}
	parameters, salt, expectedKey, ok := parseArgon2ID(encodedHash)
	if !ok {
		return false
	}
	actualKey := argon2.IDKey(
		[]byte(password), salt, parameters.iterations, parameters.memoryKiB,
		parameters.parallelism, uint32(len(expectedKey)),
	)
	return subtle.ConstantTimeCompare(actualKey, expectedKey) == 1
}

type argonParameters struct {
	memoryKiB   uint32
	iterations  uint32
	parallelism uint8
}

func parseArgon2ID(encodedHash string) (argonParameters, []byte, []byte, bool) {
	parts := strings.Split(encodedHash, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v="+strconv.Itoa(argon2.Version) {
		return argonParameters{}, nil, nil, false
	}
	var memory, iterations uint32
	var parallelism uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &iterations, &parallelism); err != nil {
		return argonParameters{}, nil, nil, false
	}
	if parts[3] != fmt.Sprintf("m=%d,t=%d,p=%d", memory, iterations, parallelism) {
		return argonParameters{}, nil, nil, false
	}
	if memory < 8*1024 || memory > 256*1024 || iterations < 1 || iterations > 10 || parallelism < 1 || parallelism > 8 {
		return argonParameters{}, nil, nil, false
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) < 8 || len(salt) > 64 {
		return argonParameters{}, nil, nil, false
	}
	key, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(key) < 16 || len(key) > 64 {
		return argonParameters{}, nil, nil, false
	}
	return argonParameters{memoryKiB: memory, iterations: iterations, parallelism: parallelism}, salt, key, true
}

func validPasswordLength(password string) bool {
	length := utf8.RuneCountInString(password)
	return utf8.ValidString(password) && length >= 8 && length <= 128
}
