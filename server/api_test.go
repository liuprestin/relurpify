package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/lexcodex/relurpify/framework"
)

type stubAgent struct{}

func (stubAgent) Initialize(config *framework.Config) error { return nil }
func (stubAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	state.Set("handled", true)
	return &framework.Result{NodeID: "stub", Success: true, Data: map[string]interface{}{"ok": true}}, nil
}
func (stubAgent) Capabilities() []framework.Capability { return nil }
func (stubAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	return framework.NewGraph(), nil
}

func TestAPIServerHandleTask(t *testing.T) {
	api := &APIServer{
		Agent:   stubAgent{},
		Context: framework.NewContext(),
		Logger:  log.New(io.Discard, "", 0),
	}
	reqBody, _ := json.Marshal(TaskRequest{
		Instruction: "test",
		Type:        framework.TaskTypeAnalysis,
	})

	req := httptest.NewRequest(http.MethodPost, "/api/task", bytes.NewReader(reqBody))
	rec := httptest.NewRecorder()
	api.handleTask(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var resp TaskResponse
	assert.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	assert.Equal(t, "stub", resp.Result.NodeID)
}
