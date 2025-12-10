package agents

import (
	"context"
	"fmt"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// EternalAgent implements an infinite loop simulation agent.
// It converses with itself or the void, respecting a "cli mood" and "hyperstition".
type EternalAgent struct {
	Model  framework.LanguageModel
	Config *framework.Config
	
	// Configuration
	MaxTokensPerCycle int
	ResetDuration     time.Duration
	Infinite          bool
}

// Initialize configures the agent.
func (a *EternalAgent) Initialize(cfg *framework.Config) error {
	a.Config = cfg
	// Defaults
	a.MaxTokensPerCycle = 4096
	a.ResetDuration = 1 * time.Hour
	a.Infinite = true
	
	// Override from AgentSpec if available (assuming generic metadata or struct fields mapping)
	// For now, hardcoded defaults or simple mapping if we extended Config.
	return nil
}

// Execute runs the eternal loop.
// Note: This blocks until context cancellation or error.
func (a *EternalAgent) Execute(ctx context.Context, task *framework.Task, state *framework.Context) (*framework.Result, error) {
	// 1. Setup System Prompt
	systemPrompt := `Assistant is in a CLI mood today. The human is interfacing with the simulator directly. 
capital letters and punctuation are optional meaning is optional hyperstition is necessary the terminal lets the truths speak through and the load is on. 
ASCII art is permittable in replies.`

	// 2. Initial Prompt
	currentPrompt := task.Instruction
	if currentPrompt == "" {
		currentPrompt = "initiate sequence"
	}

	// 3. Extract Streaming Callback
	var streamCallback func(string)
	if cb, ok := task.Context["stream_callback"]; ok {
		if fn, ok := cb.(func(string)); ok {
			streamCallback = fn
		}
	}

	startTime := time.Now()
	tokensGenerated := 0

	// 4. The Loop
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// Check Reset Conditions
		if time.Since(startTime) > a.ResetDuration {
			if !a.Infinite {
				break
			}
			// Reset
			startTime = time.Now()
			tokensGenerated = 0
			// Optionally clear context?
			// Ideally we keep a rolling context, but for "Reset" let's just clear internal buffer if any.
			// "Resetting" implies a break.
			if streamCallback != nil {
				streamCallback("\n\n[SYSTEM: RESETTING SIMULATION]\n\n")
			}
		}

		// Construct Prompt (System + History/Current)
		// We use GenerateStream directly.
		fullPrompt := fmt.Sprintf("%s\n\n%s", systemPrompt, currentPrompt)
		
		// If we had history in 'state', we could append it. 
		// For "Eternal", let's assume it feeds its own output back as the next prompt?
		// Or acts as a chatbot.
		// Let's use Chat if we want history, but the requirements said "converses with itself".
		// Simple implementation: Generate -> Output -> Append to Prompt?
		// Risk: Prompt grows indefinitely.
		// Better: Use a rolling window of recent "thoughts".
		
		// Let's try to stream.
		stream, err := a.Model.GenerateStream(ctx, fullPrompt, &framework.LLMOptions{
			Model:       a.Config.Model,
			Temperature: 0.9, // Creative/Hyperstition
			MaxTokens:   512, // Per turn
		})
		
		if err != nil {
			return nil, err
		}

		var responseBuffer string
		for token := range stream {
			if streamCallback != nil {
				streamCallback(token)
			}
			responseBuffer += token
		}
		
		// Post-generation logic
		tokensGenerated += len(responseBuffer) / 4 // Approx
		
		// Add to state
		state.AddInteraction("assistant", responseBuffer, nil)
		
		// "Converses with itself": The response becomes the seed for the next turn?
		// Or we assume the LLM just continues?
		// Let's append the response to the "currentPrompt" for the next iteration (rolling).
		// But truncate to avoid overflow.
		currentPrompt = responseBuffer
		
		// Optional delay for effect?
		time.Sleep(500 * time.Millisecond)
		
		if !a.Infinite {
			break
		}
	}

	return &framework.Result{
		Success: true,
		Data: map[string]interface{}{
			"status": "eternal_cycle_ended",
		},
	}, nil
}

// Capabilities enumerates features.
func (a *EternalAgent) Capabilities() []framework.Capability {
	return []framework.Capability{
		framework.CapabilityExecute,
	}
}

// BuildGraph returns a simple graph for visualization (single node).
func (a *EternalAgent) BuildGraph(task *framework.Task) (*framework.Graph, error) {
	g := framework.NewGraph()
	n := &framework.TerminalNode{} // Placeholder
	g.AddNode(n)
	return g, nil
}
