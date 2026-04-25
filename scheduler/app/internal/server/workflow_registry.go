package server

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"x402-scheduler/internal/scheduler"
)

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
	return discoverWorkflowSpecIndexUnder(workflowsRootDir)
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
			existingUploaded := isUploadedWorkflowPath(existing)
			candidateUploaded := isUploadedWorkflowPath(path)

			// Uploaded workflows intentionally take precedence over bundled ones,
			// regardless of traversal order.
			if existingUploaded && !candidateUploaded {
				return nil
			}
			if !existingUploaded && candidateUploaded {
				index[normalized.ID] = path
				return nil
			}
		}
		index[normalized.ID] = path
		return nil
	})
	if err != nil {
		return nil, err
	}

	return index, nil
}

func isUploadedWorkflowPath(path string) bool {
	return strings.Contains(path, string(filepath.Separator)+"uploaded"+string(filepath.Separator))
}
