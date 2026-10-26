package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"
)

var rawBase64 = base64.RawURLEncoding

type AccessClaims struct {
	Subject      string `json:"sub"`
	SessionID    string `json:"sid"`
	TokenVersion int64  `json:"token_version"`
	IssuedAt     int64  `json:"iat"`
	ExpiresAt    int64  `json:"exp"`
	Issuer       string `json:"iss"`
}

func (claims AccessClaims) UserID() (int64, error) {
	return strconv.ParseInt(claims.Subject, 10, 64)
}

func (claims AccessClaims) SessionIDValue() (int64, error) {
	return strconv.ParseInt(claims.SessionID, 10, 64)
}

type TokenManager struct {
	secret    []byte
	issuer    string
	accessTTL time.Duration
	now       func() time.Time
}

func NewTokenManager(secret, issuer string, accessTTL time.Duration) TokenManager {
	return TokenManager{
		secret: []byte(secret), issuer: issuer, accessTTL: accessTTL, now: time.Now,
	}
}

func (manager TokenManager) IssueAccess(userID, sessionID, tokenVersion int64) (string, time.Time, error) {
	now := manager.now().UTC()
	expiresAt := now.Add(manager.accessTTL)
	claims := AccessClaims{
		Subject: strconv.FormatInt(userID, 10), SessionID: strconv.FormatInt(sessionID, 10),
		TokenVersion: tokenVersion, IssuedAt: now.Unix(), ExpiresAt: expiresAt.Unix(), Issuer: manager.issuer,
	}
	header, _ := json.Marshal(struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}{Algorithm: "HS256", Type: "JWT"})
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("marshal access claims: %w", err)
	}
	unsigned := rawBase64.EncodeToString(header) + "." + rawBase64.EncodeToString(payload)
	signature := manager.sign(unsigned)
	return unsigned + "." + rawBase64.EncodeToString(signature), expiresAt, nil
}

func (manager TokenManager) VerifyAccess(token string) (AccessClaims, error) {
	if len(token) == 0 || len(token) > 4096 {
		return AccessClaims{}, ErrSessionInvalid
	}
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return AccessClaims{}, ErrSessionInvalid
	}
	providedSignature, err := rawBase64.DecodeString(parts[2])
	if err != nil || subtle.ConstantTimeCompare(manager.sign(parts[0]+"."+parts[1]), providedSignature) != 1 {
		return AccessClaims{}, ErrSessionInvalid
	}
	headerBytes, err := rawBase64.DecodeString(parts[0])
	if err != nil {
		return AccessClaims{}, ErrSessionInvalid
	}
	var header struct {
		Algorithm string `json:"alg"`
		Type      string `json:"typ"`
	}
	if err := json.Unmarshal(headerBytes, &header); err != nil || header.Algorithm != "HS256" || header.Type != "JWT" {
		return AccessClaims{}, ErrSessionInvalid
	}
	payload, err := rawBase64.DecodeString(parts[1])
	if err != nil {
		return AccessClaims{}, ErrSessionInvalid
	}
	var claims AccessClaims
	if err := json.Unmarshal(payload, &claims); err != nil {
		return AccessClaims{}, ErrSessionInvalid
	}
	now := manager.now().UTC().Unix()
	if claims.Issuer != manager.issuer || claims.TokenVersion < 1 || claims.IssuedAt > now+30 || claims.ExpiresAt <= now || claims.ExpiresAt <= claims.IssuedAt {
		return AccessClaims{}, ErrSessionInvalid
	}
	userID, userErr := claims.UserID()
	sessionID, sessionErr := claims.SessionIDValue()
	if userErr != nil || sessionErr != nil || userID < 1 || sessionID < 1 {
		return AccessClaims{}, ErrSessionInvalid
	}
	return claims, nil
}

func (manager TokenManager) sign(value string) []byte {
	mac := hmac.New(sha256.New, manager.secret)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func GenerateRefreshToken() (raw, hash string, err error) {
	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		return "", "", fmt.Errorf("generate refresh token: %w", err)
	}
	raw = rawBase64.EncodeToString(buffer)
	hash = HashRefreshToken(raw)
	return raw, hash, nil
}

func GenerateTemporaryPassword() (string, error) {
	buffer := make([]byte, 18)
	if _, err := rand.Read(buffer); err != nil {
		return "", fmt.Errorf("generate temporary password: %w", err)
	}
	return rawBase64.EncodeToString(buffer), nil
}

func HashRefreshToken(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func ValidateRefreshToken(raw string) error {
	if len(raw) != 43 {
		return ErrSessionInvalid
	}
	decoded, err := rawBase64.DecodeString(raw)
	if err != nil || len(decoded) != 32 {
		return ErrSessionInvalid
	}
	return nil
}
