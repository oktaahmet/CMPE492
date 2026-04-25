package server

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"mime"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"x402-scheduler/internal/scheduler"
	"x402-scheduler/internal/storage/postgres"
)

const serverExecutionTimeout = 45 * time.Second

func dispatchWorkflowJobs(
	ctx context.Context,
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
	jobs []scheduler.Job,
) error {
	for _, job := range jobs {
		if scheduler.NormalizeExecutionTarget(string(job.ExecutionTarget)) == scheduler.ExecutionTargetServer {
			if err := executeServerJob(ctx, engine, workflowManager, store, job); err != nil {
				return err
			}
			continue
		}
		if err := engine.Enqueue(job); err != nil {
			return err
		}
	}
	return nil
}

func executeServerJob(
	ctx context.Context,
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
	job scheduler.Job,
) error {
	runCtx, cancel := context.WithTimeout(ctx, serverExecutionTimeout)
	defer cancel()

	outputArtifacts, err := prepareServerOutputArtifacts(job)
	if err != nil {
		return err
	}
	inputContext, err := buildServerExecutionContext(runCtx, store, job, outputArtifacts)
	if err != nil {
		return err
	}
	resultPayload, err := runNativeServerNode(runCtx, job, inputContext)
	if err != nil {
		return err
	}
	finalizedArtifacts, err := finalizeServerOutputArtifacts(outputArtifacts)
	if err != nil {
		return err
	}
	if len(finalizedArtifacts) > 0 {
		resultPayload["artifacts"] = artifactMetadataMap(finalizedArtifacts)
	}
	acceptedResult, err := hashResultPayload(resultPayload)
	if err != nil {
		return err
	}
	return persistServerJobCompletion(runCtx, engine, workflowManager, store, job, acceptedResult, resultPayload)
}

func buildServerExecutionContext(
	ctx context.Context,
	store *postgres.Store,
	job scheduler.Job,
	outputArtifacts []scheduler.WorkflowArtifact,
) (map[string]any, error) {
	args := append([]any(nil), job.Args...)
	inputs := make(map[string]map[string]any, len(job.Dependencies))

	for _, dep := range job.Dependencies {
		output, found, err := store.LoadWorkflowNodeOutput(ctx, dep.WorkflowID, dep.NodeID)
		if err != nil {
			return nil, fmt.Errorf("load dependency %s/%s: %w", dep.WorkflowID, dep.NodeID, err)
		}
		if !found {
			return nil, fmt.Errorf("dependency output missing: %s/%s", dep.WorkflowID, dep.NodeID)
		}
		inputs[dep.NodeID] = output
	}

	return map[string]any{
		"args":             args,
		"inputs":           inputs,
		"artifacts":        serverArtifactContext(job.Artifacts),
		"output_artifacts": serverArtifactContext(outputArtifacts),
	}, nil
}

func serverArtifactContext(artifacts []scheduler.WorkflowArtifact) map[string]any {
	if len(artifacts) == 0 {
		return map[string]any{}
	}
	out := make(map[string]any, len(artifacts))
	for _, artifact := range artifacts {
		out[artifact.ID] = map[string]any{
			"id":     artifact.ID,
			"path":   artifact.LocalPath,
			"url":    artifact.URL,
			"size":   artifact.Size,
			"sha256": artifact.SHA256,
		}
	}
	return out
}

func runNativeServerNode(
	ctx context.Context,
	job scheduler.Job,
	inputContext map[string]any,
) (map[string]any, error) {
	inputBytes, err := json.Marshal(inputContext)
	if err != nil {
		return nil, fmt.Errorf("marshal server execution input: %w", err)
	}

	binaryPath, err := nativeExecutablePathFromWasmURL(job.WasmURL)
	if err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, binaryPath)
	cmd.Stdin = bytes.NewReader(inputBytes)

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = strings.TrimSpace(stdout.String())
		}
		if msg == "" {
			return nil, fmt.Errorf("server native node failed: %w", err)
		}
		return nil, fmt.Errorf("server native node failed: %w: %s", err, msg)
	}

	var output any
	if err := json.Unmarshal(stdout.Bytes(), &output); err != nil {
		return nil, fmt.Errorf("decode native node output: %w", err)
	}

	resultPayload := map[string]any{
		"mode":   fmt.Sprintf("server_run_json(%dB)", len(stdout.Bytes())),
		"output": output,
	}

	return resultPayload, nil
}

func prepareServerOutputArtifacts(job scheduler.Job) ([]scheduler.WorkflowArtifact, error) {
	if len(job.OutputArtifacts) == 0 {
		return nil, nil
	}
	out := make([]scheduler.WorkflowArtifact, 0, len(job.OutputArtifacts))
	for _, artifact := range job.OutputArtifacts {
		localPath, err := outputArtifactLocalPath(job.WorkflowID, job.NodeID, artifact.Path)
		if err != nil {
			return nil, fmt.Errorf("prepare output artifact %s: %w", artifact.ID, err)
		}
		if err := os.MkdirAll(filepath.Dir(localPath), 0o755); err != nil {
			return nil, fmt.Errorf("create output artifact dir %s: %w", artifact.ID, err)
		}
		if err := os.Remove(localPath); err != nil && !os.IsNotExist(err) {
			return nil, fmt.Errorf("clear stale output artifact %s: %w", artifact.ID, err)
		}
		contentType := strings.TrimSpace(artifact.ContentType)
		if contentType == "" {
			contentType = mime.TypeByExtension(filepath.Ext(localPath))
		}
		if contentType == "" {
			contentType = "application/octet-stream"
		}
		artifact.LocalPath = localPath
		artifact.URL = outputWorkflowArtifactURL(job.WorkflowID, job.NodeID, artifact.ID)
		artifact.ContentType = contentType
		out = append(out, artifact)
	}
	return out, nil
}

func finalizeServerOutputArtifacts(artifacts []scheduler.WorkflowArtifact) ([]scheduler.WorkflowArtifact, error) {
	if len(artifacts) == 0 {
		return nil, nil
	}
	out := make([]scheduler.WorkflowArtifact, 0, len(artifacts))
	for _, artifact := range artifacts {
		info, err := os.Stat(artifact.LocalPath)
		if err != nil {
			return nil, fmt.Errorf("output artifact %s was not written: %w", artifact.ID, err)
		}
		if info.IsDir() {
			return nil, fmt.Errorf("output artifact %s is a directory", artifact.ID)
		}
		sum, err := sha256File(artifact.LocalPath)
		if err != nil {
			return nil, fmt.Errorf("hash output artifact %s: %w", artifact.ID, err)
		}
		artifact.Size = info.Size()
		artifact.SHA256 = sum
		out = append(out, artifact)
	}
	return out, nil
}

func artifactMetadataMap(artifacts []scheduler.WorkflowArtifact) map[string]any {
	out := make(map[string]any, len(artifacts))
	for _, artifact := range artifacts {
		out[artifact.ID] = map[string]any{
			"id":     artifact.ID,
			"path":   artifact.Path,
			"url":    artifact.URL,
			"size":   artifact.Size,
			"sha256": artifact.SHA256,
		}
	}
	return out
}

func hashResultPayload(payload map[string]any) (string, error) {
	bytes, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("hash result payload: %w", err)
	}
	sum := sha256.Sum256(bytes)
	return hex.EncodeToString(sum[:]), nil
}

func persistServerJobCompletion(
	ctx context.Context,
	engine *scheduler.Engine,
	workflowManager *scheduler.WorkflowManager,
	store *postgres.Store,
	job scheduler.Job,
	acceptedResult string,
	output map[string]any,
) error {
	if err := store.UpsertWorkflowNodeCompletion(
		ctx,
		job.WorkflowID,
		job.NodeID,
		job.ID,
		acceptedResult,
		output,
		time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("failed to persist finalized workflow node state: %w", err)
	}

	nextJobs, err := workflowManager.OnJobFinalized(job.ID, output)
	if err != nil {
		return fmt.Errorf("failed to progress workflow: %w", err)
	}
	if err := dispatchWorkflowJobs(ctx, engine, workflowManager, store, nextJobs); err != nil {
		return fmt.Errorf("failed to enqueue unlocked workflow jobs: %w", err)
	}
	return nil
}

func nativeExecutablePathFromWasmURL(wasmURL string) (string, error) {
	relOut, err := wasmRelativePathFromURL(wasmURL)
	if err != nil {
		return "", err
	}
	stem := strings.TrimSuffix(relOut, filepath.Ext(relOut))
	return filepath.Join("native", filepath.FromSlash(stem)+nativeExecutableExt()), nil
}

func nativeExecutableExt() string {
	if runtime.GOOS == "windows" {
		return ".exe"
	}
	return ""
}
