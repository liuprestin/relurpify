package persistence

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"github.com/lexcodex/relurpify/framework"
)

// MessageStore persists interaction histories per workflow/task.
type MessageStore interface {
	Append(ctx context.Context, workflowID string, interactions ...framework.Interaction) error
	History(ctx context.Context, workflowID string) ([]framework.Interaction, error)
	Clear(ctx context.Context, workflowID string) error
}

// FileMessageStore keeps messages in JSON files.
type FileMessageStore struct {
	root string
	mu   sync.RWMutex
}

// NewFileMessageStore builds a store in the provided root directory.
func NewFileMessageStore(root string) (*FileMessageStore, error) {
	if root == "" {
		return nil, errors.New("message store root required")
	}
	if err := os.MkdirAll(root, 0o755); err != nil {
		return nil, err
	}
	return &FileMessageStore{root: root}, nil
}

func (s *FileMessageStore) pathFor(id string) string {
	return filepath.Join(s.root, id+".messages.json")
}

// Append stores interactions for a workflow.
func (s *FileMessageStore) Append(ctx context.Context, workflowID string, interactions ...framework.Interaction) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	if workflowID == "" {
		return errors.New("workflow id required")
	}
	if len(interactions) == 0 {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	existing, err := s.read(workflowID)
	if err != nil {
		return err
	}
	existing = append(existing, interactions...)
	data, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.pathFor(workflowID), data, 0o644)
}

// History returns the conversation for a workflow.
func (s *FileMessageStore) History(ctx context.Context, workflowID string) ([]framework.Interaction, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.read(workflowID)
}

// Clear removes stored messages.
func (s *FileMessageStore) Clear(ctx context.Context, workflowID string) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return os.Remove(s.pathFor(workflowID))
}

func (s *FileMessageStore) read(workflowID string) ([]framework.Interaction, error) {
	path := s.pathFor(workflowID)
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var interactions []framework.Interaction
	if err := json.Unmarshal(data, &interactions); err != nil {
		return nil, err
	}
	return interactions, nil
}
