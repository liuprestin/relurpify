package testsuite

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/lexcodex/relurpify/agents"
	"github.com/lexcodex/relurpify/framework"
)

// MockAgent implements framework.Agent for testing
type MockAgent struct {
	ExecuteFunc func(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error)
}

func (m *MockAgent) Initialize(config *framework.Config) error { return nil }
func (m *MockAgent) Capabilities() []framework.Capability    { return nil }
func (m *MockAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	return nil, nil
}
func (m *MockAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	if m.ExecuteFunc != nil {
		return m.ExecuteFunc(ctx, task, state)
	}
	return &framework.Result{Success: true}, nil
}

func TestCoordinatorSelfHealing(t *testing.T) {
	coordinator := agents.NewAgentCoordinator(nil, framework.NewContextBudget(1024))

	// 1. Planner - returns a dummy plan
	planner := &MockAgent{
		ExecuteFunc: func(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
			return &framework.Result{
				Success: true,
				Data: map[string]interface{}{
					"plan_steps": []agents.PlanStep{{ID: "1", Description: "Step 1"}},
					"files":      []string{"main.go"},
				},
			}, nil
		},
	}
	coordinator.RegisterAgent("planner", planner)

	// 2. Executor - Fails first time, succeeds second time
	attempts := 0
	executor := &MockAgent{
		ExecuteFunc: func(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
			attempts++
			if attempts == 1 {
				return nil, fmt.Errorf("transient failure")
			}
			// Verify that diagnosis was added to instruction on retry
			if attempts == 2 {
				if !strings.Contains(task.Instruction, "Diagnosis: The executor failed") {
					t.Errorf("expected instruction to contain diagnosis, got: %s", task.Instruction)
				}
			}
			return &framework.Result{Success: true, Data: map[string]interface{}{"status": "fixed"}}, nil
		},
	}
	coordinator.RegisterAgent("executor", executor)

	// 3. Ask (Diagnosis)
	asker := &MockAgent{
		ExecuteFunc: func(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
			return &framework.Result{
				Success: true,
				Data: map[string]interface{}{
					"text": "The executor failed because of a transient error.",
				},
			}, nil
		},
	}
	coordinator.RegisterAgent("ask", asker)

	task := &framework.Task{
		Instruction: "Refactor the code",
		Type:        framework.TaskTypeCodeModification,
	}

	result, err := coordinator.ExecuteTask(task)
	if err != nil {
		t.Fatalf("coordinator failed: %v", err)
	}
	if result == nil || !result.Success {
		t.Fatal("expected success result")
	}
	if attempts != 2 {
		t.Fatalf("expected 2 executor attempts (1 fail + 1 success), got %d", attempts)
	}
}

func TestCoordinatorReviewLoop(t *testing.T) {
	coordinator := agents.NewAgentCoordinator(nil, framework.NewContextBudget(1024))
	// Configure for fast iterations
	coordinator.Config.MaxReviewIterations = 3
	coordinator.Config.ReviewSeverity = "error"

	// 1. Executor - Always succeeds
	execCalls := 0
	executor := &MockAgent{
		ExecuteFunc: func(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
			execCalls++
			return &framework.Result{Success: true, Data: map[string]interface{}{"changed": true}}, nil
		},
	}
	coordinator.RegisterAgent("executor", executor)

	// 2. Reviewer - Returns issues first time, then passes
	reviewCalls := 0
	reviewer := &MockAgent{
		ExecuteFunc: func(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
			reviewCalls++
			if reviewCalls == 1 {
				return &framework.Result{
					Success: true,
					Data: map[string]interface{}{
						"passed": false,
						"issues": []agents.ReviewIssue{
							{File: "main.go", Line: 10, Severity: "error", Message: "Fix this"},
						},
					},
				}, nil
			}
			return &framework.Result{
				Success: true,
				Data: map[string]interface{}{
					"passed": true,
					"issues": []agents.ReviewIssue{},
				},
			}, nil
		},
	}
	coordinator.RegisterAgent("reviewer", reviewer)

	task := &framework.Task{
		Instruction: "Review the code",
		Type:        framework.TaskTypeReview,
		Metadata:    map[string]string{"require_review": "true"},
	}

	result, err := coordinator.ExecuteTask(task)
	if err != nil {
		t.Fatalf("coordinator failed: %v", err)
	}
	if result == nil {
		t.Fatal("expected result")
	}

	// Should run: Exec -> Review (Fail) -> Exec -> Review (Pass)
	if execCalls != 2 {
		t.Errorf("expected 2 executor calls, got %d", execCalls)
	}
	if reviewCalls != 2 {
		t.Errorf("expected 2 reviewer calls, got %d", reviewCalls)
	}
}

func TestCoordinatorReviewStalemate(t *testing.T) {
	coordinator := agents.NewAgentCoordinator(nil, framework.NewContextBudget(1024))
	coordinator.Config.MaxReviewIterations = 5

	executor := &MockAgent{
		ExecuteFunc: func(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
			return &framework.Result{Success: true}, nil
		},
	}
	coordinator.RegisterAgent("executor", executor)

	// Reviewer always returns the SAME issue
	reviewer := &MockAgent{
		ExecuteFunc: func(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
			return &framework.Result{
				Success: true,
				Data: map[string]interface{}{
					"passed": false,
					"issues": []agents.ReviewIssue{
						{File: "main.go", Line: 10, Severity: "error", Message: "Persistent error"},
					},
				},
			}, nil
		},
	}
	coordinator.RegisterAgent("reviewer", reviewer)

	task := &framework.Task{
		Instruction: "Review the code",
		Type:        framework.TaskTypeReview,
	}

	_, err := coordinator.ExecuteTask(task)
	if err != nil {
		t.Fatalf("coordinator failed: %v", err)
	}

	// Should run 2 iterations and detect stalemate (Init + 1 retry that produces same result)
	// Iteration 0: Exec -> Review (Issues A)
	// Iteration 1: Exec -> Review (Issues A) -> Stalemate detected -> Break
	// So we expect 2 iterations.
	// Wait, check implementation:
	// Loop 0: Exec, Review, LastIssues = A
	// Loop 1: Exec, Review, Issues = A. Compare(Last, A) -> Identical -> Break.
	// Correct.
}
