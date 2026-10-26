package textnorm

import (
	"errors"
	"net/mail"
	"regexp"
	"strings"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

var nonSpacingMark = regexp.MustCompile(`\p{Mn}+`)

func HumanName(value string, maxRunes int) (string, error) {
	normalized := strings.Join(strings.Fields(value), " ")
	if normalized == "" || len([]rune(normalized)) > maxRunes {
		return "", errors.New("invalid human name")
	}
	return normalized, nil
}

func Search(value string) string {
	value = strings.NewReplacer("Đ", "D", "đ", "d").Replace(value)
	value = norm.NFKD.String(value)
	value = nonSpacingMark.ReplaceAllString(value, "")
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func Optional(value string, maxRunes int) (*string, error) {
	normalized := strings.TrimSpace(value)
	if normalized == "" {
		return nil, nil
	}
	if len([]rune(normalized)) > maxRunes {
		return nil, errors.New("text is too long")
	}
	return &normalized, nil
}

func Email(value string) (*string, *string, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil, nil, nil
	}
	parsed, err := mail.ParseAddress(trimmed)
	if err != nil || parsed.Address != trimmed || len(trimmed) > 254 {
		return nil, nil, errors.New("invalid email")
	}
	normalized := strings.ToLower(trimmed)
	return &trimmed, &normalized, nil
}

func UpperCode(value string) string {
	return strings.ToUpper(strings.TrimSpace(value))
}

func Digits(value string) string {
	var output strings.Builder
	for _, character := range value {
		if unicode.IsDigit(character) && character <= unicode.MaxASCII {
			output.WriteRune(character)
		}
	}
	return output.String()
}
