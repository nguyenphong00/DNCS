package auth

import (
	"errors"
	"regexp"
	"strings"
	"unicode"
)

var (
	staffUsernamePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{2,99}$`)
	e164VNPattern        = regexp.MustCompile(`^\+84[0-9]{9,10}$`)
)

func NormalizeStaffUsername(value string) (string, error) {
	normalized := strings.ToLower(strings.TrimSpace(value))
	if !staffUsernamePattern.MatchString(normalized) {
		return "", errors.New("invalid staff username")
	}
	return normalized, nil
}

func NormalizeVietnamPhone(value string) (string, error) {
	var digits strings.Builder
	for _, character := range strings.TrimSpace(value) {
		if unicode.IsDigit(character) && character <= unicode.MaxASCII {
			digits.WriteRune(character)
			continue
		}
		if strings.ContainsRune("+ .-()", character) {
			continue
		}
		return "", errors.New("invalid phone character")
	}
	number := digits.String()
	switch {
	case strings.HasPrefix(number, "0"):
		number = "84" + strings.TrimPrefix(number, "0")
	case strings.HasPrefix(number, "84"):
	default:
		return "", errors.New("phone must use Vietnam country code")
	}
	normalized := "+" + number
	if !e164VNPattern.MatchString(normalized) {
		return "", errors.New("invalid Vietnam phone length")
	}
	return normalized, nil
}
