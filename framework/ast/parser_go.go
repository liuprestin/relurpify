package ast

import (
	"fmt"
	goast "go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"time"
)

// GoParser builds AST data using go/parser.
type GoParser struct {
	fset *token.FileSet
}

// NewGoParser returns a ready-to-use Go parser.
func NewGoParser() *GoParser {
	return &GoParser{fset: token.NewFileSet()}
}

func (gp *GoParser) Language() string          { return "go" }
func (gp *GoParser) Category() Category        { return CategoryCode }
func (gp *GoParser) SupportsIncremental() bool { return false }
func (gp *GoParser) ParseIncremental(*ParseResult, []ContentChange) (*ParseResult, error) {
	return nil, fmt.Errorf("go incremental parsing not implemented")
}

// Parse converts Go source code into AST nodes/edges.
func (gp *GoParser) Parse(content string, filePath string) (*ParseResult, error) {
	file, err := parser.ParseFile(gp.fset, filePath, content, parser.ParseComments)
	if err != nil {
		return nil, err
	}
	fileID := GenerateFileID(filePath)
	result := &ParseResult{
		Nodes: make([]*Node, 0),
		Edges: make([]*Edge, 0),
	}
	now := time.Now().UTC()
	rootNode := &Node{
		ID:        fmt.Sprintf("%s:root", fileID),
		FileID:    fileID,
		Type:      NodeTypePackage,
		Category:  CategoryCode,
		Language:  "go",
		Name:      file.Name.Name,
		StartLine: 1,
		EndLine:   gp.fset.Position(file.End()).Line,
		CreatedAt: now,
		UpdatedAt: now,
	}
	result.RootNode = rootNode
	result.Nodes = append(result.Nodes, rootNode)

	for _, imp := range file.Imports {
		importPath := strings.Trim(imp.Path.Value, "\"")
		node := &Node{
			ID:        fmt.Sprintf("%s:import:%s:%d", fileID, importPath, gp.fset.Position(imp.Pos()).Line),
			ParentID:  rootNode.ID,
			FileID:    fileID,
			Type:      NodeTypeImport,
			Category:  CategoryCode,
			Language:  "go",
			Name:      importPath,
			StartLine: gp.fset.Position(imp.Pos()).Line,
			EndLine:   gp.fset.Position(imp.End()).Line,
			CreatedAt: now,
			UpdatedAt: now,
		}
		result.Nodes = append(result.Nodes, node)
		result.Edges = append(result.Edges, &Edge{
			ID:       fmt.Sprintf("%s:imports:%s", rootNode.ID, node.ID),
			SourceID: rootNode.ID,
			TargetID: node.ID,
			Type:     EdgeTypeImports,
		})
	}

	goast.Inspect(file, func(n goast.Node) bool {
		switch decl := n.(type) {
		case *goast.FuncDecl:
			fnNode := gp.buildFunctionNode(decl, fileID, rootNode.ID)
			result.Nodes = append(result.Nodes, fnNode)
			result.Edges = append(result.Edges, gp.collectCallEdges(decl, fnNode.ID, fileID)...)
		case *goast.GenDecl:
			result.Nodes = append(result.Nodes, gp.buildGenDeclNodes(decl, fileID, rootNode.ID)...)
		}
		return true
	})

	result.Metadata = &FileMetadata{
		ID:            fileID,
		Path:          filePath,
		RelativePath:  filepath.Base(filePath),
		Language:      "go",
		Category:      CategoryCode,
		LineCount:     gp.fset.Position(file.End()).Line,
		TokenCount:    len(content),
		Complexity:    0,
		ContentHash:   HashContent(content),
		RootNodeID:    rootNode.ID,
		NodeCount:     len(result.Nodes),
		EdgeCount:     len(result.Edges),
		IndexedAt:     now,
		ParserVersion: "0.1.0",
	}
	return result, nil
}

func (gp *GoParser) buildFunctionNode(decl *goast.FuncDecl, fileID, parentID string) *Node {
	now := time.Now().UTC()
	name := decl.Name.Name
	node := &Node{
		ID:         fmt.Sprintf("%s:func:%s", fileID, name),
		ParentID:   parentID,
		FileID:     fileID,
		Type:       NodeTypeFunction,
		Category:   CategoryCode,
		Language:   "go",
		Name:       name,
		Signature:  gp.signature(decl),
		DocString:  docString(decl.Doc),
		IsExported: goast.IsExported(name),
		StartLine:  gp.fset.Position(decl.Pos()).Line,
		EndLine:    gp.fset.Position(decl.End()).Line,
		CreatedAt:  now,
		UpdatedAt:  now,
		Attributes: map[string]interface{}{},
	}
	if decl.Recv != nil && len(decl.Recv.List) > 0 {
		node.Type = NodeTypeMethod
		node.Attributes["receiver"] = fmt.Sprintf("%s", decl.Recv.List[0].Type)
	}
	return node
}

func (gp *GoParser) collectCallEdges(fn *goast.FuncDecl, sourceID, fileID string) []*Edge {
	edges := make([]*Edge, 0)
	if fn.Body == nil {
		return edges
	}
	goast.Inspect(fn.Body, func(n goast.Node) bool {
		call, ok := n.(*goast.CallExpr)
		if !ok {
			return true
		}
		var target string
		switch fun := call.Fun.(type) {
		case *goast.Ident:
			target = fun.Name
		case *goast.SelectorExpr:
			if fun.Sel != nil {
				target = fun.Sel.Name
			}
		}
		if target == "" {
			return true
		}
		edge := &Edge{
			ID:       fmt.Sprintf("%s:calls:%s:%d", sourceID, target, gp.fset.Position(call.Lparen).Line),
			SourceID: sourceID,
			TargetID: fmt.Sprintf("%s:func:%s", fileID, target),
			Type:     EdgeTypeCalls,
		}
		edges = append(edges, edge)
		return true
	})
	return edges
}

func (gp *GoParser) buildGenDeclNodes(decl *goast.GenDecl, fileID, parentID string) []*Node {
	nodes := make([]*Node, 0)
	now := time.Now().UTC()
	for _, spec := range decl.Specs {
		switch typed := spec.(type) {
		case *goast.TypeSpec:
			node := &Node{
				ID:         fmt.Sprintf("%s:type:%s", fileID, typed.Name.Name),
				ParentID:   parentID,
				FileID:     fileID,
				Category:   CategoryCode,
				Language:   "go",
				Name:       typed.Name.Name,
				IsExported: goast.IsExported(typed.Name.Name),
				StartLine:  gp.fset.Position(typed.Pos()).Line,
				EndLine:    gp.fset.Position(typed.End()).Line,
				CreatedAt:  now,
				UpdatedAt:  now,
				Attributes: map[string]interface{}{},
			}
			switch typed.Type.(type) {
			case *goast.StructType:
				node.Type = NodeTypeStruct
			case *goast.InterfaceType:
				node.Type = NodeTypeInterface
			default:
				node.Type = NodeTypeType
			}
			nodes = append(nodes, node)
		}
	}
	return nodes
}

func (gp *GoParser) signature(fn *goast.FuncDecl) string {
	builder := strings.Builder{}
	builder.WriteString("func ")
	if fn.Recv != nil {
		builder.WriteString("(")
		builder.WriteString(formatFieldList(fn.Recv))
		builder.WriteString(") ")
	}
	builder.WriteString(fn.Name.Name)
	builder.WriteString("(")
	builder.WriteString(formatFieldList(fn.Type.Params))
	builder.WriteString(")")
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		builder.WriteString(" ")
		if len(fn.Type.Results.List) == 1 && len(fn.Type.Results.List[0].Names) == 0 {
			builder.WriteString(formatField(fn.Type.Results.List[0]))
		} else {
			builder.WriteString("(")
			builder.WriteString(formatFieldList(fn.Type.Results))
			builder.WriteString(")")
		}
	}
	return builder.String()
}

func docString(comment *goast.CommentGroup) string {
	if comment == nil {
		return ""
	}
	return comment.Text()
}

func formatFieldList(list *goast.FieldList) string {
	if list == nil {
		return ""
	}
	parts := make([]string, 0, len(list.List))
	for _, field := range list.List {
		parts = append(parts, formatField(field))
	}
	return strings.Join(parts, ", ")
}

func formatField(field *goast.Field) string {
	var builder strings.Builder
	if field == nil {
		return ""
	}
	names := make([]string, 0, len(field.Names))
	for _, name := range field.Names {
		names = append(names, name.Name)
	}
	if len(names) > 0 {
		builder.WriteString(strings.Join(names, ", "))
		builder.WriteString(" ")
	}
	builder.WriteString(fmt.Sprintf("%s", field.Type))
	return builder.String()
}
