package whatsapp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// ValidateSignature checks the X-Hub-Signature-256 header against the raw body
// using the Meta app secret. Meta sends the header as "sha256=<hex>" — we
// compare in constant time.
//
// Returning false for any malformed header is intentional: a webhook that
// can't prove its origin must be rejected.
func ValidateSignature(appSecret string, header string, body []byte) bool {
	if appSecret == "" || header == "" {
		return false
	}
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	want, err := hex.DecodeString(header[len(prefix):])
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	got := mac.Sum(nil)
	return hmac.Equal(got, want)
}

// Sign returns the header value Meta expects — exposed for tests and for the
// outbound webhook signature if we ever relay to an internal system.
func Sign(appSecret string, body []byte) string {
	mac := hmac.New(sha256.New, []byte(appSecret))
	mac.Write(body)
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}
