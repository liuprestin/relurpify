package agents

import pattern "github.com/lexcodex/relurpify/agents/pattern"

// PlannerAgent re-exports the pattern-based planner so existing callers can
// continue instantiating it via the agents package.
type PlannerAgent = pattern.PlannerAgent

// ReActAgent re-exports the ReAct agent implementation.
type ReActAgent = pattern.ReActAgent

// ReflectionAgent re-exports the reviewer agent.
type ReflectionAgent = pattern.ReflectionAgent

// ModeRuntimeProfile exposes the pattern runtime profile struct.
type ModeRuntimeProfile = pattern.ModeRuntimeProfile

// ContextPreferences exposes context tuning knobs.
type ContextPreferences = pattern.ContextPreferences
