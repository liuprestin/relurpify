package persistence

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lexcodex/relurpify/framework"
)

// CheckpointStore persists graph checkpoints to disk.
type CheckpointStore struct {
	basePath string
}

// NewCheckpointStore creates a store rooted at the provided path.
func NewCheckpointStore(basePath string) *CheckpointStore {
	return &CheckpointStore{basePath: basePath}
}

// Save writes the checkpoint to disk using task/checkpoint identifiers.
func (cs *CheckpointStore) Save(checkpoint *framework.GraphCheckpoint) error {
	if checkpoint == nil {
		return fmt.Errorf("nil checkpoint")
	}
	path := filepath.Join(cs.basePath, checkpoint.TaskID, checkpoint.CheckpointID+".json")
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(checkpoint, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

// Load retrieves a checkpoint from disk.
func (cs *CheckpointStore) Load(taskID, checkpointID string) (*framework.GraphCheckpoint, error) {
	path := filepath.Join(cs.basePath, taskID, checkpointID+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var checkpoint framework.GraphCheckpoint
	if err := json.Unmarshal(data, &checkpoint); err != nil {
		return nil, err
	}
	return &checkpoint, nil
}

// List returns all checkpoint IDs stored for a task.
func (cs *CheckpointStore) List(taskID string) ([]string, error) {
	path := filepath.Join(cs.basePath, taskID)
	entries, err := os.ReadDir(path)
	if err != nil {
		return nil, err
	}
	result := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if filepath.Ext(entry.Name()) != ".json" {
			continue
		}
		result = append(result, strings.TrimSuffix(entry.Name(), ".json"))
	}
	return result, nil
}
