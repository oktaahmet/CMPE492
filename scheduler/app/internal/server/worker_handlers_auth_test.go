package server

import (
	"bytes"
	"crypto/ecdsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ethereum/go-ethereum/accounts"
	"github.com/ethereum/go-ethereum/common/hexutil"
	"github.com/ethereum/go-ethereum/crypto"

	"x402-scheduler/internal/scheduler"
)

func TestWorkerHTTPHandlersWithAuthHappyPathAndWorkerMismatch(t *testing.T) {
	engine := scheduler.NewEngine(scheduler.Config{AssignmentTTL: time.Minute})
	workflowManager := scheduler.NewWorkflowManager()
	auth, err := newWorkerAuth("test-secret", time.Hour, time.Minute)
	if err != nil {
		t.Fatalf("newWorkerAuth() error = %v", err)
	}

	ownerKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate owner key: %v", err)
	}
	otherKey, err := crypto.GenerateKey()
	if err != nil {
		t.Fatalf("generate other key: %v", err)
	}
	workerID := crypto.PubkeyToAddress(ownerKey.PublicKey).Hex()
	otherWorkerID := crypto.PubkeyToAddress(otherKey.PublicKey).Hex()
	ownerToken := issueSignedWorkerToken(t, auth, workerID, ownerKey)
	otherToken := issueSignedWorkerToken(t, auth, otherWorkerID, otherKey)

	job := scheduler.Job{
		ID:                "job-auth-flow",
		WorkflowID:        "wf-auth",
		NodeID:            "node-a",
		WasmURL:           "/node-a.wasm",
		RewardUSDC:        "0.01",
		ReplicationFactor: 2,
		ResultSchema: map[string]scheduler.PayloadFieldRule{
			"output": {Type: "string", Required: true},
		},
	}
	if err := engine.Enqueue(job); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	registerBody := mustJSONBodyAuth(t, RegisterWorkerRequest{WorkerID: workerID})
	registerReq := httptest.NewRequest(http.MethodPost, "/api/workers/register", registerBody)
	registerReq.Header.Set("Authorization", "Bearer "+ownerToken)
	registerRec := httptest.NewRecorder()
	registerWorkerHandler(engine, auth).ServeHTTP(registerRec, registerReq)
	if registerRec.Code != http.StatusOK {
		t.Fatalf("authorized register status = %d body=%s", registerRec.Code, registerRec.Body.String())
	}

	mismatchRegisterBody := mustJSONBodyAuth(t, RegisterWorkerRequest{WorkerID: workerID})
	mismatchRegisterReq := httptest.NewRequest(http.MethodPost, "/api/workers/register", mismatchRegisterBody)
	mismatchRegisterReq.Header.Set("Authorization", "Bearer "+otherToken)
	mismatchRegisterRec := httptest.NewRecorder()
	registerWorkerHandler(engine, auth).ServeHTTP(mismatchRegisterRec, mismatchRegisterReq)
	if mismatchRegisterRec.Code != http.StatusForbidden {
		t.Fatalf("expected register mismatch forbidden, got %d body=%s", mismatchRegisterRec.Code, mismatchRegisterRec.Body.String())
	}

	pullReq := httptest.NewRequest(http.MethodGet, "/api/pull?worker_id="+workerID, nil)
	pullReq.Header.Set("Authorization", "Bearer "+ownerToken)
	pullRec := httptest.NewRecorder()
	pullHandler(engine, auth).ServeHTTP(pullRec, pullReq)
	if pullRec.Code != http.StatusOK {
		t.Fatalf("authorized pull status = %d body=%s", pullRec.Code, pullRec.Body.String())
	}

	mismatchPullReq := httptest.NewRequest(http.MethodGet, "/api/pull?worker_id="+workerID, nil)
	mismatchPullReq.Header.Set("Authorization", "Bearer "+otherToken)
	mismatchPullRec := httptest.NewRecorder()
	pullHandler(engine, auth).ServeHTTP(mismatchPullRec, mismatchPullReq)
	if mismatchPullRec.Code != http.StatusForbidden {
		t.Fatalf("expected pull mismatch forbidden, got %d body=%s", mismatchPullRec.Code, mismatchPullRec.Body.String())
	}

	resultRaw := mustJSONBytesAuth(t, scheduler.ResultSubmission{
		JobID:         job.ID,
		WorkerID:      workerID,
		ResultSig:     "sig-a",
		ResultPayload: map[string]any{"output": "ok"},
	})
	resultReq := httptest.NewRequest(http.MethodPost, "/api/result", bytes.NewReader(resultRaw))
	resultReq.Header.Set("Authorization", "Bearer "+ownerToken)
	resultRec := httptest.NewRecorder()
	resultHandler(engine, workflowManager, nil, 1<<20, auth).ServeHTTP(resultRec, resultReq)
	if resultRec.Code != http.StatusOK {
		t.Fatalf("authorized result status = %d body=%s", resultRec.Code, resultRec.Body.String())
	}

	mismatchResultReq := httptest.NewRequest(http.MethodPost, "/api/result", bytes.NewReader(resultRaw))
	mismatchResultReq.Header.Set("Authorization", "Bearer "+otherToken)
	mismatchResultRec := httptest.NewRecorder()
	resultHandler(engine, workflowManager, nil, 1<<20, auth).ServeHTTP(mismatchResultRec, mismatchResultReq)
	if mismatchResultRec.Code != http.StatusForbidden {
		t.Fatalf("expected result mismatch forbidden, got %d body=%s", mismatchResultRec.Code, mismatchResultRec.Body.String())
	}
}

func issueSignedWorkerToken(t *testing.T, auth *workerAuth, workerID string, key *ecdsa.PrivateKey) string {
	t.Helper()

	challenge, err := auth.issueChallenge(workerID, "localhost:8080")
	if err != nil {
		t.Fatalf("issueChallenge() error = %v", err)
	}
	hash := accounts.TextHash([]byte(challenge.Message))
	signature, err := crypto.Sign(hash, key)
	if err != nil {
		t.Fatalf("sign challenge: %v", err)
	}
	session, err := auth.verifyChallenge(workerID, challenge.Nonce, hexutil.Encode(signature))
	if err != nil {
		t.Fatalf("verifyChallenge() error = %v", err)
	}
	return session.Token
}

func mustJSONBodyAuth(t *testing.T, value any) *bytes.Reader {
	t.Helper()
	return bytes.NewReader(mustJSONBytesAuth(t, value))
}

func mustJSONBytesAuth(t *testing.T, value any) []byte {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json body: %v", err)
	}
	return raw
}
