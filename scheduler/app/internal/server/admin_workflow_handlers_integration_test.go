package server

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"x402-scheduler/internal/scheduler"
	"x402-scheduler/internal/storage/postgres"
)

func TestAdminWorkflowUploadActivateDeleteHandlers(t *testing.T) {
	ctx := context.Background()
	store := newDisposableServerStore(t, ctx)
	if err := store.Migrate(ctx); err != nil {
		t.Fatalf("Migrate() error = %v", err)
	}

	originalWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd() error = %v", err)
	}
	tmp := t.TempDir()
	if err := os.Chdir(tmp); err != nil {
		t.Fatalf("Chdir() error = %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chdir(originalWD)
		invalidateWorkflowSpecIndexCache()
		invalidateWorkflowArtifactSpecCache()
	})

	workflowID := "wf-admin-upload"
	staticWasm := filepath.Join("static", "uploaded", workflowID, "node.wasm")
	if err := os.MkdirAll(filepath.Dir(staticWasm), 0o755); err != nil {
		t.Fatalf("mkdir static wasm dir: %v", err)
	}
	if err := os.WriteFile(staticWasm, []byte("test wasm placeholder"), 0o644); err != nil {
		t.Fatalf("write static wasm: %v", err)
	}
	future := time.Now().Add(time.Hour)
	if err := os.Chtimes(staticWasm, future, future); err != nil {
		t.Fatalf("future-date static wasm: %v", err)
	}

	engine := scheduler.NewEngine(scheduler.Config{AssignmentTTL: time.Minute})
	manager := scheduler.NewWorkflowManager()

	uploadReq := newWorkflowUploadRequest(t, workflowID)
	uploadRec := httptest.NewRecorder()
	withAdminToken("secret", adminWorkflowUploadHandler(manager)).ServeHTTP(uploadRec, uploadReq)
	if uploadRec.Code != http.StatusCreated {
		t.Fatalf("upload status = %d body=%s", uploadRec.Code, uploadRec.Body.String())
	}
	if _, err := os.Stat(uploadedWorkflowSpecPath(workflowID)); err != nil {
		t.Fatalf("uploaded workflow spec missing: %v", err)
	}

	activateBody := bytes.NewBufferString(`{"workflow_id":"` + workflowID + `","reset_state":true}`)
	activateReq := httptest.NewRequest(http.MethodPost, "/api/admin/workflows/activate", activateBody)
	activateReq.Header.Set("Authorization", "Bearer secret")
	activateRec := httptest.NewRecorder()
	withAdminToken("secret", adminWorkflowActivateHandler(engine, manager, store)).ServeHTTP(activateRec, activateReq)
	if activateRec.Code != http.StatusOK {
		t.Fatalf("activate status = %d body=%s", activateRec.Code, activateRec.Body.String())
	}
	var activateResp adminWorkflowActivateResponse
	if err := json.NewDecoder(activateRec.Body).Decode(&activateResp); err != nil {
		t.Fatalf("decode activate response: %v", err)
	}
	if activateResp.WorkflowID != workflowID || activateResp.EnqueuedJobs != 1 || activateResp.TopologicalSize != 1 {
		t.Fatalf("unexpected activate response: %#v", activateResp)
	}
	if manager.ActiveWorkflowID() != workflowID {
		t.Fatalf("expected active workflow %s, got %s", workflowID, manager.ActiveWorkflowID())
	}
	if got, err := store.GetActiveWorkflowID(ctx); err != nil || got != workflowID {
		t.Fatalf("persisted active workflow = %q, %v", got, err)
	}
	if stats := engine.StatsSnapshot(); stats.TotalJobs != 1 || stats.QueuedJobs != 1 {
		t.Fatalf("unexpected engine stats after activation: %#v", stats)
	}

	deleteBody := bytes.NewBufferString(`{"workflow_id":"` + workflowID + `"}`)
	deleteReq := httptest.NewRequest(http.MethodPost, "/api/admin/workflows/delete", deleteBody)
	deleteReq.Header.Set("Authorization", "Bearer secret")
	deleteRec := httptest.NewRecorder()
	withAdminToken("secret", adminWorkflowDeleteHandler(engine, manager, store)).ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusOK {
		t.Fatalf("delete status = %d body=%s", deleteRec.Code, deleteRec.Body.String())
	}
	if manager.ActiveWorkflowID() != "" {
		t.Fatalf("expected workflow manager to be cleared, got %s", manager.ActiveWorkflowID())
	}
	if got, err := store.GetActiveWorkflowID(ctx); err != nil || got != "" {
		t.Fatalf("expected persisted active workflow to be cleared, got %q err=%v", got, err)
	}
	if _, err := os.Stat(uploadedWorkflowDir(workflowID)); !os.IsNotExist(err) {
		t.Fatalf("expected uploaded workflow directory to be deleted, stat err=%v", err)
	}
}

func newWorkflowUploadRequest(t *testing.T, workflowID string) *http.Request {
	t.Helper()

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	workflowJSON := `{
		"id":"` + workflowID + `",
		"nodes":[{
			"id":"node",
			"program":"node.cpp",
			"execution_target":"browser_worker",
			"replication_factor":2,
			"result_schema":{"output":{"type":"string","required":true}},
			"reward_usdc":"0.01"
		}]
	}`
	if err := addMultipartFile(writer, "workflow_json", "workflow.json", workflowJSON); err != nil {
		t.Fatalf("add workflow_json part: %v", err)
	}
	if err := addMultipartFile(writer, "cpp_files", "node.cpp", "int main(){return 0;}\n"); err != nil {
		t.Fatalf("add cpp_files part: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close multipart writer: %v", err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/admin/workflows/upload", &body)
	req.Header.Set("Authorization", "Bearer secret")
	req.Header.Set("Content-Type", writer.FormDataContentType())
	return req
}

func addMultipartFile(writer *multipart.Writer, fieldName string, fileName string, content string) error {
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		return err
	}
	_, err = part.Write([]byte(content))
	return err
}

func newDisposableServerStore(t *testing.T, ctx context.Context) *postgres.Store {
	t.Helper()

	adminDSN := strings.TrimSpace(os.Getenv("TEST_DATABASE_URL"))
	if adminDSN == "" {
		t.Skip("set TEST_DATABASE_URL to run admin workflow integration tests")
	}

	adminDB, err := sql.Open("pgx", adminDSN)
	if err != nil {
		t.Fatalf("open admin db: %v", err)
	}

	dbName := fmt.Sprintf("x402_scheduler_server_test_%d", time.Now().UnixNano())
	if _, err := adminDB.ExecContext(ctx, `CREATE DATABASE `+dbName); err != nil {
		_ = adminDB.Close()
		t.Fatalf("create disposable db: %v", err)
	}

	store, err := postgres.NewStore(databaseDSNForTest(adminDSN, dbName))
	if err != nil {
		_, _ = adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS `+dbName)
		_ = adminDB.Close()
		t.Fatalf("NewStore() error = %v", err)
	}

	t.Cleanup(func() {
		_ = store.Close()
		_, _ = adminDB.ExecContext(ctx, `SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname = $1`, dbName)
		_, _ = adminDB.ExecContext(ctx, `DROP DATABASE IF EXISTS `+dbName)
		_ = adminDB.Close()
	})

	return store
}

func databaseDSNForTest(base string, dbName string) string {
	parsed, err := url.Parse(base)
	if err != nil {
		return base
	}
	parsed.Path = "/" + dbName
	return parsed.String()
}
