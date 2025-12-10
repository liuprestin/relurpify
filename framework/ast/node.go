package ast

import "time"

// Node represents a structured unit extracted from a file.
type Node struct {
	ID         string                 `json:"id"`
	ParentID   string                 `json:"parent_id"`
	FileID     string                 `json:"file_id"`
	Type       NodeType               `json:"type"`
	Category   Category               `json:"category"`
	Language   string                 `json:"language"`
	StartLine  int                    `json:"start_line"`
	EndLine    int                    `json:"end_line"`
	StartCol   int                    `json:"start_col"`
	EndCol     int                    `json:"end_col"`
	Name       string                 `json:"name"`
	Signature  string                 `json:"signature"`
	DocString  string                 `json:"doc_string"`
	Attributes map[string]interface{} `json:"attributes"`
	IsExported bool                   `json:"is_exported"`
	// IsDeprecated can be toggled by parsers that understand deprecation tags.
	IsDeprecated bool      `json:"is_deprecated"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
	ContentHash  string    `json:"content_hash"`
}

// NodeType enumerates supported node kinds.
type NodeType string

const (
	NodeTypePackage   NodeType = "package"
	NodeTypeImport    NodeType = "import"
	NodeTypeFunction  NodeType = "function"
	NodeTypeMethod    NodeType = "method"
	NodeTypeClass     NodeType = "class"
	NodeTypeInterface NodeType = "interface"
	NodeTypeStruct    NodeType = "struct"
	NodeTypeVariable  NodeType = "variable"
	NodeTypeConstant  NodeType = "constant"
	NodeTypeEnum      NodeType = "enum"

	NodeTypeDocument  NodeType = "document"
	NodeTypeSection   NodeType = "section"
	NodeTypeHeading   NodeType = "heading"
	NodeTypeCodeBlock NodeType = "code_block"
	NodeTypeParagraph NodeType = "paragraph"
	NodeTypeTable     NodeType = "table"
	NodeTypeList      NodeType = "list"
	NodeTypeLink      NodeType = "link"

	NodeTypeConfigRoot NodeType = "config_root"
	NodeTypeObject     NodeType = "object"
	NodeTypeArray      NodeType = "array"
	NodeTypeProperty   NodeType = "property"
	NodeTypeReference  NodeType = "reference"

	NodeTypeSchema     NodeType = "schema"
	NodeTypeTableDecl  NodeType = "table"
	NodeTypeColumn     NodeType = "column"
	NodeTypeIndexDecl  NodeType = "index"
	NodeTypeConstraint NodeType = "constraint"
	NodeTypeType       NodeType = "type"
	NodeTypeField      NodeType = "field"

	NodeTypeResource   NodeType = "resource"
	NodeTypeModule     NodeType = "module"
	NodeTypeOutput     NodeType = "output"
	NodeTypeDataSource NodeType = "data_source"
)

// Category represents the high level grouping for a node.
type Category string

const (
	CategoryCode   Category = "code"
	CategoryDoc    Category = "document"
	CategoryConfig Category = "config"
	CategorySchema Category = "schema"
	CategoryInfra  Category = "infrastructure"
)

// Edge represents a typed relationship between nodes.
type Edge struct {
	ID         string                 `json:"id"`
	SourceID   string                 `json:"source_id"`
	TargetID   string                 `json:"target_id"`
	Type       EdgeType               `json:"type"`
	Attributes map[string]interface{} `json:"attributes"`
}

// EdgeType enumerates relationship classes.
type EdgeType string

const (
	EdgeTypeCalls      EdgeType = "calls"
	EdgeTypeImports    EdgeType = "imports"
	EdgeTypeReferences EdgeType = "references"
	EdgeTypeImplements EdgeType = "implements"
	EdgeTypeExtends    EdgeType = "extends"
	EdgeTypeContains   EdgeType = "contains"
	EdgeTypeLinks      EdgeType = "links"
	EdgeTypeIncludes   EdgeType = "includes"
	EdgeTypeDependsOn  EdgeType = "depends_on"
	EdgeTypeConfigures EdgeType = "configures"
	EdgeTypeForeignKey EdgeType = "foreign_key"
	EdgeTypeHasMany    EdgeType = "has_many"
	EdgeTypeBelongsTo  EdgeType = "belongs_to"
)
