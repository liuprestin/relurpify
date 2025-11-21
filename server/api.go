package server

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// APIServer exposes HTTP endpoints for testing agents without an editor.
type APIServer struct {
	Agent   framework.Agent
	Context *framework.Context
	Logger  *log.Logger
}

// TaskRequest describes incoming API payload.
type TaskRequest struct {
	Instruction string                 `json:"instruction"`
	Type        framework.TaskType     `json:"type"`
	Context     map[string]interface{} `json:"context"`
}

// TaskResponse describes API response.
type TaskResponse struct {
	Result *framework.Result `json:"result"`
	Error  string            `json:"error,omitempty"`
}

// Serve starts listening on the provided address.
func (s *APIServer) Serve(addr string) error {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/task", s.handleTask)
	mux.HandleFunc("/api/context", s.handleContext)
	server := &http.Server{
		Addr:    addr,
		Handler: mux,
	}
	if s.Logger != nil {
		s.Logger.Printf("API listening on %s", addr)
	}
	return server.ListenAndServe()
}

func (s *APIServer) handleTask(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	var req TaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if req.Type == "" {
		req.Type = framework.TaskTypeCodeModification
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Minute)
	defer cancel()
	task := &framework.Task{
		ID:          time.Now().Format("20060102150405"),
		Type:        req.Type,
		Instruction: req.Instruction,
		Context:     req.Context,
	}
	state := s.Context.Clone()
	result, err := s.Agent.Execute(ctx, task, state)
	resp := TaskResponse{Result: result}
	if err != nil {
		resp.Error = err.Error()
	}
	if err == nil {
		s.Context.Merge(state)
	}
	writeJSON(w, resp)
}

func (s *APIServer) handleContext(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.Context)
}

func writeJSON(w http.ResponseWriter, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}
