package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common"
	"github.com/ethereum/go-ethereum/crypto"
	"github.com/golang-jwt/jwt/v5"
)

const workerJWTIssuer = "x402-scheduler-worker-auth"

type WorkerAuthChallengeRequest struct {
	WorkerID string `json:"worker_id"`
}

type WorkerAuthChallengeResponse struct {
	WorkerID  string    `json:"worker_id"`
	Nonce     string    `json:"nonce"`
	Message   string    `json:"message"`
	ExpiresAt time.Time `json:"expires_at"`
}

type WorkerAuthVerifyRequest struct {
	WorkerID  string `json:"worker_id"`
	Nonce     string `json:"nonce"`
	Signature string `json:"signature"`
}

type WorkerAuthVerifyResponse struct {
	WorkerID  string    `json:"worker_id"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

type workerJWTClaims struct {
	WorkerID string `json:"worker_id"`
	jwt.RegisteredClaims
}

type workerChallenge struct {
	WorkerID  string
	Nonce     string
	Message   string
	ExpiresAt time.Time
}

type workerAuth struct {
	jwtSecret    []byte
	tokenTTL     time.Duration
	challengeTTL time.Duration
	now          func() time.Time

	mu         sync.Mutex
	challenges map[string]workerChallenge
}

func newWorkerAuth(jwtSecret string, tokenTTL time.Duration, challengeTTL time.Duration) (*workerAuth, error) {
	if tokenTTL <= 0 {
		tokenTTL = 12 * time.Hour
	}
	if challengeTTL <= 0 {
		challengeTTL = 5 * time.Minute
	}

	secret := strings.TrimSpace(jwtSecret)
	if secret == "" {
		buf := make([]byte, 32)
		if _, err := rand.Read(buf); err != nil {
			return nil, fmt.Errorf("generate worker jwt secret: %w", err)
		}
		secret = hex.EncodeToString(buf)
		log.Println("WORKER_JWT_SECRET not set: generated ephemeral secret for worker auth")
	}

	return &workerAuth{
		jwtSecret:    []byte(secret),
		tokenTTL:     tokenTTL,
		challengeTTL: challengeTTL,
		now:          time.Now,
		challenges:   map[string]workerChallenge{},
	}, nil
}

func normalizeWorkerID(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("worker_id is required")
	}
	if !common.IsHexAddress(trimmed) {
		return "", fmt.Errorf("worker_id must be valid wallet address: %s", trimmed)
	}
	return common.HexToAddress(trimmed).Hex(), nil
}

func sameWorkerID(left string, right string) bool {
	return strings.EqualFold(strings.TrimSpace(left), strings.TrimSpace(right))
}

func (a *workerAuth) issueChallenge(workerID string, host string) (WorkerAuthChallengeResponse, error) {
	normalized, err := normalizeWorkerID(workerID)
	if err != nil {
		return WorkerAuthChallengeResponse{}, err
	}

	now := a.now().UTC()
	expiresAt := now.Add(a.challengeTTL)
	nonce, err := randomHex(16)
	if err != nil {
		return WorkerAuthChallengeResponse{}, fmt.Errorf("generate auth nonce: %w", err)
	}

	message := fmt.Sprintf(
		"Sign in to x402 scheduler browser worker.\n\nThis is not a blockchain transaction.\nNo gas or funds will be spent.\nThis signature only proves that you control this wallet address.\n\nWallet: %s\nNonce: %s\nIssued At: %s\nExpires At: %s",
		normalized,
		nonce,
		now.Format(time.RFC3339),
		expiresAt.Format(time.RFC3339),
	)

	a.mu.Lock()
	defer a.mu.Unlock()
	a.cleanupExpiredChallengesLocked(now)
	a.challenges[nonce] = workerChallenge{
		WorkerID:  normalized,
		Nonce:     nonce,
		Message:   message,
		ExpiresAt: expiresAt,
	}

	return WorkerAuthChallengeResponse{
		WorkerID:  normalized,
		Nonce:     nonce,
		Message:   message,
		ExpiresAt: expiresAt,
	}, nil
}

func (a *workerAuth) verifyChallenge(workerID string, nonce string, signature string) (WorkerAuthVerifyResponse, error) {
	normalized, err := normalizeWorkerID(workerID)
	if err != nil {
		return WorkerAuthVerifyResponse{}, err
	}

	nonce = strings.TrimSpace(nonce)
	if nonce == "" {
		return WorkerAuthVerifyResponse{}, errors.New("nonce is required")
	}

	signature = strings.TrimSpace(signature)
	if signature == "" {
		return WorkerAuthVerifyResponse{}, errors.New("signature is required")
	}

	now := a.now().UTC()

	a.mu.Lock()
	a.cleanupExpiredChallengesLocked(now)
	challenge, ok := a.challenges[nonce]
	a.mu.Unlock()

	if !ok || !sameWorkerID(challenge.WorkerID, normalized) {
		return WorkerAuthVerifyResponse{}, errors.New("challenge not found")
	}
	if !challenge.ExpiresAt.After(now) {
		return WorkerAuthVerifyResponse{}, errors.New("challenge expired")
	}
	if err := verifyWalletSignature(challenge.Message, signature, normalized); err != nil {
		return WorkerAuthVerifyResponse{}, err
	}

	a.mu.Lock()
	if current, exists := a.challenges[nonce]; exists && current.Message == challenge.Message && sameWorkerID(current.WorkerID, challenge.WorkerID) {
		delete(a.challenges, nonce)
	}
	a.mu.Unlock()

	token, expiresAt, err := a.issueToken(normalized)
	if err != nil {
		return WorkerAuthVerifyResponse{}, err
	}

	return WorkerAuthVerifyResponse{
		WorkerID:  normalized,
		Token:     token,
		ExpiresAt: expiresAt,
	}, nil
}

func (a *workerAuth) issueToken(workerID string) (string, time.Time, error) {
	now := a.now().UTC()
	expiresAt := now.Add(a.tokenTTL)
	claims := workerJWTClaims{
		WorkerID: workerID,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    workerJWTIssuer,
			Subject:   workerID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expiresAt),
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	signed, err := token.SignedString(a.jwtSecret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign worker jwt: %w", err)
	}
	return signed, expiresAt, nil
}

func (a *workerAuth) authenticateRequest(r *http.Request) (string, error) {
	header := strings.TrimSpace(r.Header.Get("Authorization"))
	if header == "" {
		return "", errors.New("missing bearer token")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return "", errors.New("invalid bearer token")
	}

	tokenString := strings.TrimSpace(strings.TrimPrefix(header, prefix))
	if tokenString == "" {
		return "", errors.New("invalid bearer token")
	}

	claims := &workerJWTClaims{}
	token, err := jwt.ParseWithClaims(tokenString, claims, func(token *jwt.Token) (any, error) {
		if token.Method == nil || token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			if token.Method == nil {
				return nil, errors.New("unexpected signing method: <nil>")
			}
			return nil, fmt.Errorf("unexpected signing method: %s", token.Method.Alg())
		}
		return a.jwtSecret, nil
	})
	if err != nil || !token.Valid {
		return "", errors.New("invalid bearer token")
	}
	if claims.Issuer != workerJWTIssuer {
		return "", errors.New("invalid bearer token")
	}
	normalized, err := normalizeWorkerID(claims.WorkerID)
	if err != nil {
		return "", errors.New("invalid bearer token")
	}
	return normalized, nil
}

func (a *workerAuth) cleanupExpiredChallengesLocked(now time.Time) {
	for nonce, challenge := range a.challenges {
		if !challenge.ExpiresAt.After(now) {
			delete(a.challenges, nonce)
		}
	}
}

func verifyWalletSignature(message string, signatureHex string, expectedWorkerID string) error {
	signature := common.FromHex(strings.TrimSpace(signatureHex))
	if len(signature) != 65 {
		return errors.New("signature must be 65 bytes")
	}

	if signature[64] >= 27 {
		signature[64] -= 27
	}
	if signature[64] > 1 {
		return errors.New("invalid signature recovery id")
	}

	hash := accounts.TextHash([]byte(message))
	pubKey, err := crypto.SigToPub(hash, signature)
	if err != nil {
		return fmt.Errorf("recover wallet address from signature: %w", err)
	}

	recovered := crypto.PubkeyToAddress(*pubKey).Hex()
	if !sameWorkerID(recovered, expectedWorkerID) {
		return errors.New("signature does not match worker wallet")
	}
	return nil
}

func randomHex(byteCount int) (string, error) {
	buf := make([]byte, byteCount)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func workerAuthChallengeHandler(auth *workerAuth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req WorkerAuthChallengeRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		resp, err := auth.issueChallenge(req.WorkerID, r.Host)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		writeJSON(w, resp, http.StatusOK)
	}
}

func workerAuthVerifyHandler(auth *workerAuth) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var req WorkerAuthVerifyRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, "invalid json body", http.StatusBadRequest)
			return
		}

		resp, err := auth.verifyChallenge(req.WorkerID, req.Nonce, req.Signature)
		if err != nil {
			http.Error(w, err.Error(), http.StatusUnauthorized)
			return
		}
		writeJSON(w, resp, http.StatusOK)
	}
}
