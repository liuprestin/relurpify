package ast

import (
	"path/filepath"
)

// LanguageDetector maps filenames/extensions to languages.
type LanguageDetector struct {
	extensionMap map[string]string
	filenameMap  map[string]string
}

// NewLanguageDetector seeds defaults for popular formats.
func NewLanguageDetector() *LanguageDetector {
	ld := &LanguageDetector{
		extensionMap: make(map[string]string),
		filenameMap:  make(map[string]string),
	}
	ld.extensionMap[".go"] = "go"
	ld.extensionMap[".py"] = "python"
	ld.extensionMap[".js"] = "javascript"
	ld.extensionMap[".ts"] = "typescript"
	ld.extensionMap[".java"] = "java"
	ld.extensionMap[".c"] = "c"
	ld.extensionMap[".cpp"] = "cpp"
	ld.extensionMap[".rs"] = "rust"
	ld.extensionMap[".md"] = "markdown"
	ld.extensionMap[".rst"] = "restructuredtext"
	ld.extensionMap[".adoc"] = "asciidoc"
	ld.extensionMap[".txt"] = "plaintext"
	ld.extensionMap[".yaml"] = "yaml"
	ld.extensionMap[".yml"] = "yaml"
	ld.extensionMap[".json"] = "json"
	ld.extensionMap[".toml"] = "toml"
	ld.extensionMap[".xml"] = "xml"
	ld.extensionMap[".ini"] = "ini"
	ld.extensionMap[".tf"] = "terraform"
	ld.extensionMap[".sql"] = "sql"
	ld.extensionMap[".graphql"] = "graphql"
	ld.extensionMap[".proto"] = "protobuf"
	ld.filenameMap["Dockerfile"] = "docker"
	ld.filenameMap["docker-compose.yml"] = "docker-compose"
	return ld
}

// Detect returns the best-effort language identifier.
func (ld *LanguageDetector) Detect(path string) string {
	if path == "" {
		return "unknown"
	}
	base := filepath.Base(path)
	if lang, ok := ld.filenameMap[base]; ok {
		return lang
	}
	if lang, ok := ld.extensionMap[filepath.Ext(base)]; ok {
		return lang
	}
	return "unknown"
}

// DetectCategory maps a language to its category.
func (ld *LanguageDetector) DetectCategory(language string) Category {
	switch language {
	case "go", "python", "javascript", "typescript", "java", "c", "cpp", "rust":
		return CategoryCode
	case "markdown", "restructuredtext", "plaintext", "asciidoc":
		return CategoryDoc
	case "yaml", "json", "toml", "xml", "ini":
		return CategoryConfig
	case "sql", "graphql", "protobuf":
		return CategorySchema
	case "terraform", "docker", "docker-compose":
		return CategoryInfra
	default:
		return CategoryDoc
	}
}
