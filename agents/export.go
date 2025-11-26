package agents

import (
	coderpkg "github.com/lexcodex/relurpify/agents/coder"
	plannerpkg "github.com/lexcodex/relurpify/agents/planner"
	reactpkg "github.com/lexcodex/relurpify/agents/react"
	reflectionpkg "github.com/lexcodex/relurpify/agents/reflection"
)

// Re-export primary agent types so existing imports continue to compile.
type (
	CodingAgent       = coderpkg.CodingAgent
	ManualCodingAgent = coderpkg.ManualCodingAgent
	ExpertCoderAgent  = coderpkg.ExpertCoderAgent
	CodingAnalysis    = coderpkg.CodingAnalysis
	CoderState        = coderpkg.CoderState
	PlannerAgent      = plannerpkg.PlannerAgent
	ReActAgent        = reactpkg.ReActAgent
	ReflectionAgent   = reflectionpkg.ReflectionAgent
)
