package agents

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// AgentCoordinator manages multiple agents with shared context.
type AgentCoordinator struct {
	agents        map[string]framework.Agent
	sharedContext *framework.SharedContext
	contextBroker *ContextBroker
	telemetry     framework.Telemetry
	Config        CoordinatorConfig
}

// CoordinatorConfig holds tuning parameters for the coordinator.
type CoordinatorConfig struct {
	MaxRecoveryAttempts int
	MaxReviewIterations int
	ReviewSeverity      string // "error", "warning", "info"
}

// ContextBroker manages context sharing between agents.
type ContextBroker struct {
	mu sync.RWMutex

	indexerCache   map[string]interface{}
	plannerPlan    *PlanContext
	executorFocus  *ExecutorContext
	reviewerIssues []ReviewIssue

	contextManager *framework.ContextManager
	budget         *framework.ContextBudget
}

// PlanContext captures planner output.
type PlanContext struct {
	Steps        []PlanStep
	Files        []string
	Dependencies map[string][]string
}

// PlanStep describes an execution step.
type PlanStep struct {
	ID              string
	Description     string
	Files           []string
	EstimatedTokens int
}

// ExecutorContext tracks executor focus.
type ExecutorContext struct {
	CurrentFile   string
	LoadedFiles   map[string]DetailLevel
	ModifiedFiles []string
}

// ReviewIssue records reviewer findings.
type ReviewIssue struct {
	File     string
	Line     int
	Severity string
	Message  string
}

// NewAgentCoordinator builds an agent coordinator with shared context.
func NewAgentCoordinator(telemetry framework.Telemetry, budget *framework.ContextBudget) *AgentCoordinator {
	if budget == nil {
		budget = framework.NewContextBudget(8192)
	}
	shared := framework.NewSharedContext(framework.NewContext(), budget, &framework.SimpleSummarizer{})
	return &AgentCoordinator{
		agents:        make(map[string]framework.Agent),
		sharedContext: shared,
		contextBroker: &ContextBroker{
			indexerCache:   make(map[string]interface{}),
			executorFocus:  &ExecutorContext{LoadedFiles: make(map[string]DetailLevel)},
			contextManager: framework.NewContextManager(budget),
			budget:         budget,
		},
		telemetry: telemetry,
		Config: CoordinatorConfig{
			MaxRecoveryAttempts: 3,
			MaxReviewIterations: 5,
			ReviewSeverity:      "error",
		},
	}
}

// RegisterAgent adds an agent to coordination pool.
func (ac *AgentCoordinator) RegisterAgent(name string, agent framework.Agent) {
	ac.agents[name] = agent
}

// Execute implements the agent execution interface, allowing the coordinator to be used as a sub-agent.
func (ac *AgentCoordinator) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	if task == nil {
		return nil, fmt.Errorf("task is required")
	}
	
	// If external state is provided, we sync it with our internal shared context
	if state != nil {
		ac.sharedContext.Context.Merge(state)
	}
	
	strategy := ac.determineStrategy(task)
	var result *framework.Result
	var err error

	switch strategy {
	case "plan_execute":
		result, err = ac.executePlanExecuteStrategy(task)
	case "explore_modify":
		result, err = ac.executeExploreModifyStrategy(task)
	case "review_iterate":
		result, err = ac.executeReviewIterateStrategy(task)
	default:
		result, err = ac.executeSingleAgentStrategy(task)
	}

	// Sync back to external state if successful
	if state != nil && err == nil {
		state.Merge(ac.sharedContext.Context)
	}
	return result, err
}

// ExecuteTask coordinates multiple agents to complete a task.
func (ac *AgentCoordinator) ExecuteTask(task *framework.Task) (*framework.Result, error) {
	return ac.Execute(context.Background(), task, nil)
}

func (ac *AgentCoordinator) executePlanExecuteStrategy(task *framework.Task) (*framework.Result, error) {
	indexer, ok := ac.agents["indexer"]
	if ok {
		ac.emitEvent("indexer_start")
		indexTask := cloneTask(task)
		indexTask.Instruction = "Update codebase index with latest changes"
		if _, err := indexer.Execute(context.Background(), indexTask, ac.sharedContext.Context); err != nil {
			return nil, fmt.Errorf("indexer failed: %w", err)
		}
		ac.contextBroker.CacheIndexResults(ac.sharedContext.Context)
	}

	planner, ok := ac.agents["planner"]
	if !ok {
		return nil, fmt.Errorf("planner agent not registered")
	}
	ac.emitEvent("planner_start")
	ac.contextBroker.LoadSummariesIntoContext(ac.sharedContext.Context)
	planTask := cloneTask(task)
	planResult, err := planner.Execute(context.Background(), planTask, ac.sharedContext.Context)
	if err != nil {
		return nil, fmt.Errorf("planner failed: %w", err)
	}
	plan := ac.contextBroker.ExtractPlan(planResult)

	executor, ok := ac.agents["executor"]
	if !ok {
		return nil, fmt.Errorf("executor agent not registered")
	}
	ac.emitEvent("executor_start")
	ac.contextBroker.LoadFullFilesForPlan(ac.sharedContext.Context, plan)

	// Execute Plan Steps
	// We treat the plan as a dependency graph.
	// 1. Mark all steps as pending.
	// 2. Loop until all completed.
	// 3. Find steps where all dependencies are completed.
	// 4. Run them in parallel (if >1).
	
	completedSteps := make(map[string]bool)
	stepMap := make(map[string]PlanStep)
	for _, s := range plan.Steps {
		stepMap[s.ID] = s
	}

	// Safety break
	maxLoops := len(plan.Steps) * 2
	loops := 0

	for len(completedSteps) < len(plan.Steps) {
		loops++
		if loops > maxLoops {
			return nil, fmt.Errorf("plan execution stuck (cycle or dependency error)")
		}

		var readySteps []PlanStep
		for _, step := range plan.Steps {
			if completedSteps[step.ID] {
				continue
			}
			// Check dependencies
			ready := true
			if deps, hasDeps := plan.Dependencies[step.ID]; hasDeps {
				for _, depID := range deps {
					if !completedSteps[depID] {
						ready = false
						break
					}
				}
			}
			if ready {
				readySteps = append(readySteps, step)
			}
		}

		if len(readySteps) == 0 {
			if len(completedSteps) < len(plan.Steps) {
				return nil, fmt.Errorf("deadlock in plan execution")
			}
			break
		}

		// Execute ready steps
		// If 1 step, run inline. If multiple, run parallel.
		if len(readySteps) == 1 {
			step := readySteps[0]
			if err := ac.executeSingleStep(context.Background(), step, executor, task, plan); err != nil {
				return nil, err
			}
			completedSteps[step.ID] = true
		} else {
			var wg sync.WaitGroup
			errChan := make(chan error, len(readySteps))
			
			for _, step := range readySteps {
				wg.Add(1)
				step := step
				go func() {
					defer wg.Done()
					// Clone context for isolation
					branchCtx := ac.sharedContext.Context.Clone()
					
					// We need a thread-safe way to run the agent. 
					// Agents are stateless usually, but we need to ensure we don't race on shared resources if tools aren't safe.
					// Most framework tools are safe (file locks, etc).
					
					// Create a transient coordinator/wrapper to run this step?
					// No, just call executor.Execute.
					
					sErr := ac.executeSingleStep(context.Background(), step, executor, task, plan)
					if sErr != nil {
						errChan <- sErr
						return
					}
					
					// In a real implementation we would merge branchCtx back.
					// For now, we assume steps are modifying FS state (side effects), 
					// so we don't strictly need to merge memory unless they output new variables.
					// To be safe, we acquire lock and merge "step results" only?
					// framework.Context.Merge handles this.
					ac.sharedContext.Context.Merge(branchCtx)
				}()
			}
			wg.Wait()
			close(errChan)
			for err := range errChan {
				if err != nil {
					return nil, err // Fail fast on parallel error
				}
			}
			for _, s := range readySteps {
				completedSteps[s.ID] = true
			}
		}
	}

	// Aggregate result (for the reviewer)
	execResult := &framework.Result{
		Success: true,
		Data: map[string]any{
			"steps_completed": len(completedSteps),
		},
	}

	reviewer, ok := ac.agents["reviewer"]
	if ok {
		ac.emitEvent("reviewer_start")
		reviewTask := cloneTask(task)
		reviewTask.Instruction = "Review the changes made"
		if reviewTask.Context == nil {
			reviewTask.Context = map[string]any{}
		}
		reviewTask.Context["original_result"] = execResult
		if reviewResult, err := reviewer.Execute(context.Background(), reviewTask, ac.sharedContext.Context); err == nil {
			ac.contextBroker.StoreReviewIssues(reviewResult)
		} else if ac.telemetry != nil {
			ac.telemetry.Emit(framework.Event{
				Type:      "reviewer_failed",
				Timestamp: timeNow(),
				Metadata: map[string]interface{}{
					"error": err.Error(),
				},
			})
		}
	}
	return execResult, nil
}

func (ac *AgentCoordinator) executeSingleStep(ctx context.Context, step PlanStep, executor framework.Agent, originalTask *framework.Task, plan *PlanContext) error {
	stepTask := cloneTask(originalTask)
	if stepTask.Context == nil {
		stepTask.Context = make(map[string]any)
	}
	// Focus instruction
	stepTask.Instruction = fmt.Sprintf("Execute step %s: %s\nFiles: %v", step.ID, step.Description, step.Files)
	stepTask.Context["plan"] = plan
	stepTask.Context["current_step"] = step
	
	// Retry logic per step
	var stepErr error
	for attempt := 0; attempt <= ac.Config.MaxRecoveryAttempts; attempt++ {
		if attempt > 0 {
			stepTask.Instruction += fmt.Sprintf("\nRetry %d: Last error: %v", attempt, stepErr)
			
			// Add diagnostic info if available
			if diagAgent, hasDiag := ac.agents["ask"]; hasDiag && stepErr != nil {
				diagTask := cloneTask(originalTask)
				diagTask.Instruction = fmt.Sprintf("Analyze why this error occurred: %v", stepErr)
				if diagRes, dErr := diagAgent.Execute(ctx, diagTask, ac.sharedContext.Context); dErr == nil {
					if diagnosis, ok := diagRes.Data["text"].(string); ok {
						stepTask.Instruction += fmt.Sprintf("\nDiagnosis: %s", diagnosis)
					}
				}
			}
		}
		res, err := executor.Execute(ctx, stepTask, ac.sharedContext.Context)
		if err == nil && res.Success {
			return nil
		}
		stepErr = err
		if stepErr == nil && !res.Success {
			stepErr = fmt.Errorf("step failed without error")
		}
		ac.emitEvent("executor_retry")
	}
	return fmt.Errorf("step %s failed: %w", step.ID, stepErr)
}

func (ac *AgentCoordinator) executeExploreModifyStrategy(task *framework.Task) (*framework.Result, error) {
	asker, ok := ac.agents["ask"]
	if ok {
		exploreTask := cloneTask(task)
		exploreTask.Instruction = fmt.Sprintf("Explore codebase to understand: %s", task.Instruction)
		if exploreResult, err := asker.Execute(context.Background(), exploreTask, ac.sharedContext.Context); err == nil {
			ac.contextBroker.CacheExplorationResults(exploreResult)
		}
	}
	executor, ok := ac.agents["executor"]
	if !ok {
		return nil, fmt.Errorf("executor agent not registered")
	}
	return executor.Execute(context.Background(), task, ac.sharedContext.Context)
}

func (ac *AgentCoordinator) executeReviewIterateStrategy(task *framework.Task) (*framework.Result, error) {
	executor, ok := ac.agents["executor"]
	reviewer, rok := ac.agents["reviewer"]
	if !ok || !rok {
		return nil, fmt.Errorf("executor or reviewer not registered")
	}
	var result *framework.Result
	var err error
	var lastIssues []ReviewIssue

	for iteration := 0; iteration < ac.Config.MaxReviewIterations; iteration++ {
		result, err = executor.Execute(context.Background(), task, ac.sharedContext.Context)
		if err != nil {
			return nil, err
		}
		reviewTask := cloneTask(task)
		reviewTask.Instruction = "Review changes and identify issues"
		if reviewTask.Context == nil {
			reviewTask.Context = map[string]any{}
		}
		reviewTask.Context["iteration"] = iteration
		reviewResult, err := reviewer.Execute(context.Background(), reviewTask, ac.sharedContext.Context)
		if err != nil {
			break
		}
		if passed, ok := reviewResult.Data["passed"].(bool); ok && passed {
			break
		}
		ac.contextBroker.StoreReviewIssues(reviewResult)
		
		issues, hasIssues := reviewResult.Data["issues"].([]ReviewIssue)
		if !hasIssues || len(issues) == 0 {
			break
		}

		// Filter issues by severity
		var criticalIssues []ReviewIssue
		for _, issue := range issues {
			if isSeverityCritical(issue.Severity, ac.Config.ReviewSeverity) {
				criticalIssues = append(criticalIssues, issue)
			}
		}

		if len(criticalIssues) == 0 {
			break
		}

		// Stalemate detection: if issues are identical to last time, stop
		if areIssuesIdentical(lastIssues, criticalIssues) {
			ac.emitEvent("review_stalemate")
			break
		}
		lastIssues = criticalIssues

		if task.Context == nil {
			task.Context = map[string]any{}
		}
		task.Context["review_issues"] = criticalIssues
		
		// Update instruction to focus on fixing issues
		var issueDesc strings.Builder
		issueDesc.WriteString("Fix the following review issues:\n")
		for _, issue := range criticalIssues {
			issueDesc.WriteString(fmt.Sprintf("- %s:%d: %s\n", issue.File, issue.Line, issue.Message))
		}
		task.Instruction = issueDesc.String()
	}
	return result, nil
}

func isSeverityCritical(issueSeverity, configSeverity string) bool {
	levels := map[string]int{"info": 0, "warning": 1, "error": 2, "critical": 3}
	return levels[strings.ToLower(issueSeverity)] >= levels[strings.ToLower(configSeverity)]
}

func areIssuesIdentical(a, b []ReviewIssue) bool {
	if len(a) != len(b) {
		return false
	}
	// Simple O(N^2) check is fine for small issue counts
	for i := range a {
		found := false
		for j := range b {
			if a[i] == b[j] {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}

func (ac *AgentCoordinator) executeSingleAgentStrategy(task *framework.Task) (*framework.Result, error) {
	executor, ok := ac.agents["executor"]
	if ok {
		return executor.Execute(context.Background(), task, ac.sharedContext.Context)
	}
	for _, agent := range ac.agents {
		return agent.Execute(context.Background(), task, ac.sharedContext.Context)
	}
	return nil, fmt.Errorf("no agents registered")
}

func (ac *AgentCoordinator) determineStrategy(task *framework.Task) string {
	if task.Metadata != nil {
		if strategy, ok := task.Metadata["strategy"]; ok && strategy != "" {
			return strategy
		}
	}

	instruction := strings.ToLower(task.Instruction)
	if strings.Contains(instruction, "refactor") ||
		strings.Contains(instruction, "redesign") ||
		strings.Contains(instruction, "architecture") {
		return "plan_execute"
	}
	if strings.Contains(instruction, "explore") ||
		strings.Contains(instruction, "understand") ||
		strings.Contains(instruction, "explain") {
		return "explore_modify"
	}
	reqReview := false
	if task.Metadata != nil {
		reqReview = strings.ToLower(task.Metadata["require_review"]) == "true"
	}
	if strings.Contains(instruction, "review") ||
		strings.Contains(instruction, "improve") ||
		reqReview {
		return "review_iterate"
	}
	return "single_agent"
}

func (ac *AgentCoordinator) emitEvent(name string) {
	if ac.telemetry == nil {
		return
	}
	ac.telemetry.Emit(framework.Event{
		Type:      framework.EventType(name),
		Timestamp: timeNow(),
	})
}

// ContextBroker helpers.
func (cb *ContextBroker) CacheIndexResults(ctx *framework.Context) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if summaries, ok := ctx.Get("ast_summaries"); ok {
		cb.indexerCache["ast_summaries"] = summaries
	}
}

func (cb *ContextBroker) LoadSummariesIntoContext(ctx *framework.Context) {
	cb.mu.RLock()
	defer cb.mu.RUnlock()
	if summaries, ok := cb.indexerCache["ast_summaries"]; ok {
		ctx.Set("loaded_summaries", summaries)
	}
}

func (cb *ContextBroker) ExtractPlan(result *framework.Result) *PlanContext {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if result == nil {
		return nil
	}
	plan := &PlanContext{
		Steps:        make([]PlanStep, 0),
		Files:        make([]string, 0),
		Dependencies: make(map[string][]string),
	}
	if steps, ok := result.Data["plan_steps"].([]PlanStep); ok {
		plan.Steps = steps
	}
	if files, ok := result.Data["files"].([]string); ok {
		plan.Files = files
	}
	cb.plannerPlan = plan
	return plan
}

func (cb *ContextBroker) LoadFullFilesForPlan(ctx *framework.Context, plan *PlanContext) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if plan == nil {
		return
	}
	for _, file := range plan.Files {
		cb.executorFocus.LoadedFiles[file] = DetailFull
	}
	ctx.Set("executor_files", plan.Files)
}

func (cb *ContextBroker) StoreReviewIssues(result *framework.Result) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if result == nil {
		return
	}
	if issues, ok := result.Data["issues"].([]ReviewIssue); ok {
		cb.reviewerIssues = issues
	}
}

func (cb *ContextBroker) CacheExplorationResults(result *framework.Result) {
	cb.mu.Lock()
	defer cb.mu.Unlock()
	if result != nil {
		cb.indexerCache["exploration"] = result.Data
	}
}

func cloneTask(task *framework.Task) *framework.Task {
	if task == nil {
		return nil
	}
	clone := *task
	if task.Context != nil {
		clone.Context = make(map[string]any, len(task.Context))
		for k, v := range task.Context {
			clone.Context[k] = v
		}
	}
	if task.Metadata != nil {
		clone.Metadata = make(map[string]string, len(task.Metadata))
		for k, v := range task.Metadata {
			clone.Metadata[k] = v
		}
	}
	return &clone
}

func timeNow() time.Time {
	return time.Now().UTC()
}
