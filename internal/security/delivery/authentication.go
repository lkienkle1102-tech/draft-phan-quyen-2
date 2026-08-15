// Package delivery provides Gin security middleware.
package delivery

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"example.com/phan-quyen-golang/internal/security/application"
	"example.com/phan-quyen-golang/internal/security/domain"
	"github.com/gin-gonic/gin"
)

type tokenClaims struct {
	Sub, Iss                            string
	Aud                                 []string
	Exp, NBF, AuthTime                  int64
	ActorType, ClientID, OrganizationID string
	AMR                                 []string
	Attributes                          map[string]string
}

type tokenHeader struct {
	Algorithm string `json:"alg"`
	Type      string `json:"typ"`
}
type rawClaims struct {
	Sub            string            `json:"sub"`
	Iss            string            `json:"iss"`
	Aud            json.RawMessage   `json:"aud"`
	Exp            int64             `json:"exp"`
	NBF            int64             `json:"nbf"`
	AuthTime       int64             `json:"auth_time"`
	ActorType      string            `json:"actor_type"`
	ClientID       string            `json:"client_id"`
	OrganizationID string            `json:"organization_id"`
	AMR            []string          `json:"amr"`
	Attributes     map[string]string `json:"attributes"`
}
type Verifier struct {
	issuer, audience string
	secret           []byte
}

func NewVerifier(issuer, audience, secret string) *Verifier {
	return &Verifier{issuer: issuer, audience: audience, secret: []byte(secret)}
}

func (v *Verifier) Verify(raw string) (domain.Actor, error) {
	parts := strings.Split(raw, ".")
	if len(parts) != 3 {
		return domain.Actor{}, errors.New("malformed token")
	}
	if err := validateHeader(parts[0]); err != nil {
		return domain.Actor{}, err
	}
	if !v.validSignature(parts) {
		return domain.Actor{}, errors.New("invalid signature")
	}
	claims, err := decodeClaims(parts[1])
	if err != nil {
		return domain.Actor{}, err
	}
	if err := v.validateClaims(claims); err != nil {
		return domain.Actor{}, err
	}
	actorType := domain.ActorType(claims.ActorType)
	return domain.Actor{ID: claims.Sub, Type: actorType, ClientID: claims.ClientID, OrganizationID: claims.OrganizationID, AMR: claims.AMR, Attributes: claims.Attributes, AuthTime: time.Unix(claims.AuthTime, 0).UTC()}, nil
}

func validateHeader(value string) error {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return err
	}
	var header tokenHeader
	if err := json.Unmarshal(payload, &header); err != nil {
		return err
	}
	if header.Algorithm != "HS256" || header.Type != "JWT" {
		return errors.New("unsupported token algorithm")
	}
	return nil
}

func (v *Verifier) validSignature(parts []string) bool {
	mac := hmac.New(sha256.New, v.secret)
	_, _ = mac.Write([]byte(parts[0] + "." + parts[1]))
	signature, err := base64.RawURLEncoding.DecodeString(parts[2])
	return err == nil && hmac.Equal(signature, mac.Sum(nil))
}
func decodeClaims(value string) (tokenClaims, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return tokenClaims{}, err
	}
	var raw rawClaims
	if err := json.Unmarshal(payload, &raw); err != nil {
		return tokenClaims{}, err
	}
	audience, err := decodeAudience(raw.Aud)
	return tokenClaims{Sub: raw.Sub, Iss: raw.Iss, Aud: audience, Exp: raw.Exp, NBF: raw.NBF, AuthTime: raw.AuthTime, ActorType: raw.ActorType, ClientID: raw.ClientID, OrganizationID: raw.OrganizationID, AMR: raw.AMR, Attributes: raw.Attributes}, err
}
func decodeAudience(raw json.RawMessage) ([]string, error) {
	var values []string
	if json.Unmarshal(raw, &values) == nil {
		return values, nil
	}
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	return []string{value}, nil
}
func (v *Verifier) validateClaims(c tokenClaims) error {
	now := time.Now().Unix()
	if c.Sub == "" || c.Iss != v.issuer || !contains(c.Aud, v.audience) || c.Exp <= now || c.NBF > now {
		return errors.New("invalid claims")
	}
	kind := domain.ActorType(c.ActorType)
	if kind != domain.ActorUser && kind != domain.ActorMachine {
		return errors.New("invalid actor")
	}
	if kind == domain.ActorMachine && c.ClientID == "" {
		return errors.New("client required")
	}
	return nil
}
func contains(values []string, wanted string) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

type Authentication struct {
	verifier *Verifier
	facts    application.Facts
}

func NewAuthentication(verifier *Verifier, facts application.Facts) *Authentication {
	return &Authentication{verifier: verifier, facts: facts}
}
func (m *Authentication) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, err := bearer(c.GetHeader("Authorization"))
		if err != nil {
			abort(c, http.StatusUnauthorized, domain.DenyUnauthenticated)
			return
		}
		actor, err := m.verifier.Verify(raw)
		if err != nil {
			abort(c, http.StatusUnauthorized, domain.DenyUnauthenticated)
			return
		}
		active, err := m.facts.ActorActive(c.Request.Context(), actor)
		if err != nil || !active {
			abort(c, http.StatusUnauthorized, domain.DenyUnauthenticated)
			return
		}
		setActor(c, actor)
		c.Next()
	}
}
func bearer(value string) (string, error) {
	parts := strings.Fields(value)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return "", errors.New("bearer required")
	}
	return parts[1], nil
}
