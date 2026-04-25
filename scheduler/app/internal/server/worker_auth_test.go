package server

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"
)

func TestWorkerAuthIssueVerifyAndAuthenticate(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	workerID := crypto.PubkeyToAddress(key.PublicKey).Hex()

	auth, err := newWorkerAuth("test-secret", time.Hour, time.Minute)
	if err != nil {
		t.Fatalf("newWorkerAuth: %v", err)
	}

	challenge, err := auth.issueChallenge(workerID, "localhost:8080")
	if err != nil {
		t.Fatalf("issueChallenge: %v", err)
	}

	hash := accounts.TextHash([]byte(challenge.Message))
	signature, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("sign challenge: %v", err)
	}

	session, err := auth.verifyChallenge(workerID, challenge.Nonce, hexutil.Encode(signature))
	if err != nil {
		t.Fatalf("verifyChallenge: %v", err)
	}
	if session.WorkerID != workerID {
		t.Fatalf("unexpected worker id: %s", session.WorkerID)
	}

	req := httptest.NewRequest("GET", "/api/payments", nil)
	req.Header.Set("Authorization", "Bearer "+session.Token)
	authenticatedWorkerID, err := auth.authenticateRequest(req)
	if err != nil {
		t.Fatalf("authenticateRequest: %v", err)
	}
	if authenticatedWorkerID != workerID {
		t.Fatalf("unexpected authenticated worker id: %s", authenticatedWorkerID)
	}
}

func TestWorkerAuthRejectsWrongSignature(t *testing.T) {
	ownerKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate owner key: %v", err)
	}
	attackerKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate attacker key: %v", err)
	}

	workerID := crypto.PubkeyToAddress(ownerKey.PublicKey).Hex()
	auth, err := newWorkerAuth("test-secret", time.Hour, time.Minute)
	if err != nil {
		t.Fatalf("newWorkerAuth: %v", err)
	}

	challenge, err := auth.issueChallenge(workerID, "localhost:8080")
	if err != nil {
		t.Fatalf("issueChallenge: %v", err)
	}

	hash := accounts.TextHash([]byte(challenge.Message))
	signature, err := crypto.Sign(hash, attackerKey)
	if err != nil {
		t.Fatalf("sign challenge: %v", err)
	}

	if _, err := auth.verifyChallenge(workerID, challenge.Nonce, hexutil.Encode(signature)); err == nil {
		t.Fatal("expected wrong signature to be rejected")
	}
}

func TestWorkerAuthRejectsReplayAndExpiredChallenge(t *testing.T) {
	key, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	workerID := crypto.PubkeyToAddress(key.PublicKey).Hex()

	baseTime := time.Date(2026, time.March, 21, 12, 0, 0, 0, time.UTC)
	auth, err := newWorkerAuth("test-secret", time.Hour, time.Minute)
	if err != nil {
		t.Fatalf("newWorkerAuth: %v", err)
	}
	auth.now = func() time.Time { return baseTime }

	challenge, err := auth.issueChallenge(workerID, "localhost:8080")
	if err != nil {
		t.Fatalf("issueChallenge: %v", err)
	}

	hash := accounts.TextHash([]byte(challenge.Message))
	signature, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("sign challenge: %v", err)
	}
	encodedSig := hexutil.Encode(signature)

	if _, err := auth.verifyChallenge(workerID, challenge.Nonce, encodedSig); err != nil {
		t.Fatalf("first verifyChallenge: %v", err)
	}
	if _, err := auth.verifyChallenge(workerID, challenge.Nonce, encodedSig); err == nil {
		t.Fatal("expected replayed challenge to be rejected")
	}

	expiredChallenge, err := auth.issueChallenge(workerID, "localhost:8080")
	if err != nil {
		t.Fatalf("issueChallenge expired: %v", err)
	}
	expiredHash := accounts.TextHash([]byte(expiredChallenge.Message))
	expiredSig, err := crypto.Sign(expiredHash, key)
	if err != nil {
		t.Fatalf("sign expired challenge: %v", err)
	}

	auth.now = func() time.Time { return baseTime.Add(2 * time.Minute) }
	if _, err := auth.verifyChallenge(workerID, expiredChallenge.Nonce, hexutil.Encode(expiredSig)); err == nil {
		t.Fatal("expected expired challenge to be rejected")
	}
}
