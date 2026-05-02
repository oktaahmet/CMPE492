package server

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"x402-scheduler/internal/scheduler"
)

const workflowSpecIndexCacheTTL = 5 * time.Second

// workflowSpecIndexCache avoids walking/parsing every workflow JSON for hot
// lookup paths. Upload/delete operations invalidate it immediately; the short
// TTL also lets manual filesystem edits appear without restarting the server.
var workflowSpecIndexCache = struct {
	sync.Mutex
	index     map[string]string
	expiresAt time.Time
}{}

func discoverWorkflowIDs() ([]string, error) {
	index, err := discoverWorkflowSpecIndex()
	if err != nil {
		return nil, err
	}

	ids := make([]string, 0, len(index))
	for id := range index {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids, nil
}

func resolveWorkflowSpecPathByID(workflowID string) (string, error) {
	index, err := discoverWorkflowSpecIndex()
	if err != nil {
		return "", err
	}
	path, ok := index[strings.TrimSpace(workflowID)]
	if !ok {
		return "", os.ErrNotExist
	}
	return path, nil
}

func discoverWorkflowSpecIndex() (map[string]string, error) {
	now := time.Now()
	workflowSpecIndexCache.Lock()
	if workflowSpecIndexCache.index != nil && now.Before(workflowSpecIndexCache.expiresAt) {
		index := cloneWorkflowSpecIndex(workflowSpecIndexCache.index)
		workflowSpecIndexCache.Unlock()
		return index, nil
	}
	workflowSpecIndexCache.Unlock()

	index, err := discoverWorkflowSpecIndexUnder(workflowsRootDir)
	if err != nil {
		return nil, err
	}

	workflowSpecIndexCache.Lock()
	workflowSpecIndexCache.index = cloneWorkflowSpecIndex(index)
	workflowSpecIndexCache.expiresAt = now.Add(workflowSpecIndexCacheTTL)
	workflowSpecIndexCache.Unlock()
	return cloneWorkflowSpecIndex(index), nil
}

func discoverWorkflowSpecIndexUnder(rootDir string) (map[string]string, error) {
	index := map[string]string{}

	if _, err := os.Stat(rootDir); err != nil {
		if os.IsNotExist(err) {
			return index, nil
		}
		return nil, err
	}

	err := filepath.WalkDir(rootDir, func(path string, d os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if d.IsDir() {
			return nil
		}
		if strings.ToLower(filepath.Ext(path)) != ".json" {
			return nil
		}

		spec, err := readWorkflowSpecFromPath(path)
		if err != nil {
			return nil
		}
		spec, err = resolveWorkflowPrograms(spec, path)
		if err != nil {
			return nil
		}
		normalized, err := scheduler.ValidateWorkflowSpec(spec)
		if err != nil {
			return nil
		}
		if !isSafeWorkflowID(normalized.ID) {
			return nil
		}

		if existing, exists := index[normalized.ID]; exists {
			existingUploaded := isUploadedWorkflowPath(rootDir, existing)
			candidateUploaded := isUploadedWorkflowPath(rootDir, path)

			// Uploaded workflows intentionally take precedence over bundled ones,
			// regardless of traversal order.
			if existingUploaded && !candidateUploaded {
				return nil
			}
			if !existingUploaded && candidateUploaded {
				index[normalized.ID] = path
				return nil
			}
			return fmt.Errorf("duplicate workflow id %s: %s and %s", normalized.ID, existing, path)
		}
		index[normalized.ID] = path
		return nil
	})
	if err != nil {
		return nil, err
	}

	return index, nil
}

func invalidateWorkflowSpecIndexCache() {
	workflowSpecIndexCache.Lock()
	workflowSpecIndexCache.index = nil
	workflowSpecIndexCache.expiresAt = time.Time{}
	workflowSpecIndexCache.Unlock()
}

func cloneWorkflowSpecIndex(index map[string]string) map[string]string {
	out := make(map[string]string, len(index))
	for id, path := range index {
		out[id] = path
	}
	return out
}

func isUploadedWorkflowPath(rootDir string, candidatePath string) bool {
	// Use a directory-boundary check instead of substring matching so paths like
	// "not-uploaded" do not accidentally count as uploaded workflows.
	inside, err := pathInsideDir(candidatePath, filepath.Join(rootDir, "uploaded"))
	return err == nil && inside
}
