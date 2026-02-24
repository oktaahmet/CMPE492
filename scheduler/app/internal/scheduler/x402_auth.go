// Creates and signs jwt for faciliator verify/settle api calls.
// Parallels: https://github.com/coinbase/cdp-sdk/blob/8794662b60e721852bfb60801a1d0bb1bb6e4c59/go/auth/jwt.go

package scheduler

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type cdpJWTSigner struct {
	apiKeyID string
	key      ed25519.PrivateKey
}

func newCDPJWTSigner(apiKeyID, privateKey string) (*cdpJWTSigner, error) {
	if apiKeyID == "" {
		return nil, errors.New("cdp api key id is required")
	}
	if privateKey == "" {
		return nil, errors.New("cdp private key is required")
	}

	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(privateKey))
	if err != nil {
		return nil, fmt.Errorf("invalid cdp ed25519 private key: %w", err)
	}

	var edKey ed25519.PrivateKey
	switch len(raw) {
	case ed25519.SeedSize:
		edKey = ed25519.NewKeyFromSeed(raw)
	case ed25519.PrivateKeySize:
		edKey = ed25519.PrivateKey(raw)
	default:
		return nil, fmt.Errorf("unexpected ed25519 key length: %d", len(raw))
	}

	return &cdpJWTSigner{
		apiKeyID: apiKeyID,
		key:      edKey,
	}, nil
}

func (s *cdpJWTSigner) Sign(method, host, path string) (string, error) {
	if host == "" || path == "" {
		return "", errors.New("request host and path are required for cdp jwt")
	}

	m := strings.ToUpper(strings.TrimSpace(method))

	uri := fmt.Sprintf("%s %s%s", m, host, path)

	now := time.Now().UTC()

	nonceHex, err := randomNonceHex(16)
	if err != nil {
		return "", err
	}

	claims := jwt.MapClaims{
		"sub":  s.apiKeyID,
		"iss":  "cdp",
		"aud":  []string{"cdp_service"},
		"uris": []string{uri},

		"nbf": now.Unix(),
		"iat": now.Unix(),
		"exp": now.Add(120 * time.Second).Unix(),
	}

	token := jwt.NewWithClaims(jwt.SigningMethodEdDSA, claims)
	token.Header["kid"] = s.apiKeyID
	token.Header["nonce"] = nonceHex
	token.Header["typ"] = "JWT"

	signed, err := token.SignedString(s.key)
	if err != nil {
		return "", fmt.Errorf("failed to sign cdp jwt: %w", err)
	}
	return signed, nil
}

func randomNonceHex(nBytes int) (string, error) {
	if nBytes <= 0 {
		nBytes = 16
	}
	b := make([]byte, nBytes)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("failed to generate nonce: %w", err)
	}
	return hex.EncodeToString(b), nil
}
