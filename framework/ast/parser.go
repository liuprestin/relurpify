package ast

// Parser converts file contents into AST nodes.
type Parser interface {
	Parse(content string, filePath string) (*ParseResult, error)
	ParseIncremental(oldAST *ParseResult, changes []ContentChange) (*ParseResult, error)
	Language() string
	Category() Category
	SupportsIncremental() bool
}

// ParseResult captures nodes, edges, and metadata.
type ParseResult struct {
	RootNode *Node
	Nodes    []*Node
	Edges    []*Edge
	Errors   []ParseError
	Metadata *FileMetadata
}

// ParseError represents parser warnings/errors.
type ParseError struct {
	Line    int    `json:"line"`
	Column  int    `json:"column"`
	Message string `json:"message"`
	Level   string `json:"level"`
}

// ContentChange describes an incremental edit.
type ContentChange struct {
	StartLine int
	EndLine   int
	StartCol  int
	EndCol    int
	OldText   string
	NewText   string
}

// ParserRegistry keeps parser implementations keyed by language.
type ParserRegistry struct {
	parsers map[string]Parser
}

// NewParserRegistry constructs a registry.
func NewParserRegistry() *ParserRegistry {
	return &ParserRegistry{parsers: make(map[string]Parser)}
}

// Register adds a parser keyed by its Language.
func (pr *ParserRegistry) Register(parser Parser) {
	if parser == nil {
		return
	}
	pr.parsers[parser.Language()] = parser
}

// GetParser retrieves a parser by language identifier.
func (pr *ParserRegistry) GetParser(language string) (Parser, bool) {
	parser, ok := pr.parsers[language]
	return parser, ok
}

// SupportedLanguages returns all registered languages.
func (pr *ParserRegistry) SupportedLanguages() []string {
	langs := make([]string, 0, len(pr.parsers))
	for lang := range pr.parsers {
		langs = append(langs, lang)
	}
	return langs
}
