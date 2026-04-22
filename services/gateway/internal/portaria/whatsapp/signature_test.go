package whatsapp

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestValidateSignature_RoundTrip(t *testing.T) {
	secret := "super-secret"
	body := []byte(`{"object":"whatsapp_business_account"}`)
	header := Sign(secret, body)

	assert.True(t, ValidateSignature(secret, header, body))
}

func TestValidateSignature_Tamper(t *testing.T) {
	secret := "super-secret"
	body := []byte(`{"object":"whatsapp_business_account"}`)
	header := Sign(secret, body)

	// flip a byte
	tampered := append([]byte{}, body...)
	tampered[1] = 'X'
	assert.False(t, ValidateSignature(secret, header, tampered))
}

func TestValidateSignature_WrongSecret(t *testing.T) {
	body := []byte(`{}`)
	header := Sign("secret-a", body)
	assert.False(t, ValidateSignature("secret-b", header, body))
}

func TestValidateSignature_BadFormat(t *testing.T) {
	body := []byte(`{}`)
	cases := []string{"", "sha1=abc", "sha256=not-hex", "sha256="}
	for _, h := range cases {
		t.Run(h, func(t *testing.T) {
			assert.False(t, ValidateSignature("secret", h, body))
		})
	}
}

func TestValidateSignature_EmptySecretAlwaysFalse(t *testing.T) {
	body := []byte(`{}`)
	header := Sign("secret", body)
	assert.False(t, ValidateSignature("", header, body))
}
