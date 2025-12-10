package framework

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// RenderToolsToPrompt converts tool definitions into a schema-like string.
// This is used when the LLM does not support native tool calling API.
func RenderToolsToPrompt(tools []Tool) string {
	if len(tools) == 0 {
		return "No tools available."
	}
	var b strings.Builder
	b.WriteString("You have access to the following tools. To call a tool, return a JSON object with 'tool' (name) and 'arguments' (map).\n\n")
	
	for _, tool := range tools {
		b.WriteString(fmt.Sprintf("## %s\n", tool.Name()))
		b.WriteString(fmt.Sprintf("%s\n", tool.Description()))
		b.WriteString("Arguments:\n")
		params := tool.Parameters()
		if len(params) == 0 {
			b.WriteString("  (No arguments)\n")
		} else {
			for _, param := range params {
				req := "optional"
				if param.Required {
					req = "required"
				}
				b.WriteString(fmt.Sprintf("  - %s (%s, %s): %s\n", param.Name, param.Type, req, param.Description))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("Example Call:\n")
	b.WriteString("```json\n{\"tool\": \"tool_name\", \"arguments\": {\"arg1\": \"value\"}}\n```\n")
	return b.String()
}

// ParseToolCallsFromText extracts potential tool calls from raw LLM output.
// It looks for JSON blocks that match the tool call schema.
func ParseToolCallsFromText(text string) []ToolCall {
	var calls []ToolCall
	
	// Attempt to find Markdown JSON blocks
	jsonBlockRegex := regexp.MustCompile("`json\n(.*?)\n`")
	matches := jsonBlockRegex.FindAllStringSubmatch(text, -1)
	
	for _, match := range matches {
		if len(match) > 1 {
			call, ok := tryParseSingleToolCall(match[1])
			if ok {
				calls = append(calls, call)
			}
		}
	}
	
	// Also attempt to parse the entire text if it looks like JSON
	// This covers cases where the model only outputs JSON without markdown code fences
	trimmed := strings.TrimSpace(text)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		if call, ok := tryParseSingleToolCall(trimmed); ok {
			calls = append(calls, call)
		}
	}

	return calls
}

func tryParseSingleToolCall(jsonText string) (ToolCall, bool) {
	var raw struct {
		Tool      string                 `json:"tool"`
		Name      string                 `json:"name"` // alias for 'tool'
		Arguments map[string]interface{} `json:"arguments"`
		Args      map[string]interface{} `json:"args"` // alias for 'arguments'
	}
	
	if err := json.Unmarshal([]byte(jsonText), &raw); err != nil {
		return ToolCall{}, false
	}
	
	name := raw.Tool
	if name == "" {
		name = raw.Name
	}
	if name == "" {
		return ToolCall{}, false
	}
	
	args := raw.Arguments
	if args == nil {
		args = raw.Args
	}
	if args == nil {
		args = make(map[string]interface{})
	}
	
	return ToolCall{
		Name: name,
		Args: args,
	},
		true
}
