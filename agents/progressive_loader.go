package agents

import (
	contextual "github.com/lexcodex/relurpify/agents/contextual"
	"github.com/lexcodex/relurpify/framework"
	"github.com/lexcodex/relurpify/framework/ast"
)

type ProgressiveLoader = contextual.ProgressiveLoader

func NewProgressiveLoader(
	contextManager *framework.ContextManager,
	indexManager *ast.IndexManager,
	searchEngine *framework.SearchEngine,
	budget *framework.ContextBudget,
	summarizer framework.Summarizer,
) *ProgressiveLoader {
	return contextual.NewProgressiveLoader(contextManager, indexManager, searchEngine, budget, summarizer)
}
