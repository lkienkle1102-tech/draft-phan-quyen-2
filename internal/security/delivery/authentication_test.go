package delivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"testing"
	"time"
)

func TestVerifierRejectsMisleadingAlgorithmHeader(t *testing.T) {
	verifier := NewVerifier("issuer", "api", "secret")
	token := signedToken(`{"alg":"none","typ":"JWT"}`, "secret")
	if _, err := verifier.Verify(token); err == nil {
		t.Fatal("misleading JWT algorithm was accepted")
	}
}

func TestVerifierAcceptsHS256JWTHeader(t *testing.T) {
	verifier := NewVerifier("issuer", "api", "secret")
	token := signedToken(`{"alg":"HS256","typ":"JWT"}`, "secret")
	if _, err := verifier.Verify(token); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
}

func signedToken(header, secret string) string {
	headerPart := base64.RawURLEncoding.EncodeToString([]byte(header))
	now := time.Now()
	claims := fmt.Sprintf(`{"sub":"user","iss":"issuer","aud":"api","exp":%d,"nbf":%d,"actor_type":"user","auth_time":%d}`, now.Add(time.Hour).Unix(), now.Add(-time.Minute).Unix(), now.Unix())
	payload := base64.RawURLEncoding.EncodeToString([]byte(claims))
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(headerPart + "." + payload))
	return headerPart + "." + payload + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
