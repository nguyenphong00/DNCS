package auth

import (
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestClientInfoUsesDirectPeerAndSanitizesUserAgent(t *testing.T) {
	request := httptest.NewRequest("GET", "/", nil)
	request.RemoteAddr = "203.0.113.9:4567"
	request.Header.Set("X-Forwarded-For", "198.51.100.1")
	request.Header.Set("User-Agent", strings.Repeat("á", 300)+"\r\nsecret")
	client := ClientInfoFromRequest(request)
	if client.IPAddress != "203.0.113.9" {
		t.Fatalf("client IP trusted spoofable forwarding header: %q", client.IPAddress)
	}
	if len(client.UserAgent) > 512 || !utf8.ValidString(client.UserAgent) || strings.ContainsAny(client.UserAgent, "\r\n") {
		t.Fatalf("unsafe user agent length=%d valid=%v value=%q", len(client.UserAgent), utf8.ValidString(client.UserAgent), client.UserAgent)
	}
}
