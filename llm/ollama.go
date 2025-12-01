package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/lexcodex/relurpify/framework"
)

// Client implements framework.LanguageModel for Ollama.
type Client struct {
	Endpoint string
	Model    string
	client   *http.Client
	Debug    bool
}

type toolFunction struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description,omitempty"`
	Parameters  map[string]interface{} `json:"parameters,omitempty"`
}

type toolDef struct {
	Type     string       `json:"type"`
	Function toolFunction `json:"function"`
}

type ollamaToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Type      string          `json:"type"`
	Arguments json.RawMessage `json:"arguments"`
	Function  struct {
		Name      string          `json:"name"`
		Arguments json.RawMessage `json:"arguments"`
	} `json:"function"`
}

type ollamaMessage struct {
	Role      string           `json:"role"`
	Content   string           `json:"content"`
	ToolCalls []ollamaToolCall `json:"tool_calls"`
}

type ollamaResponse struct {
	Text            string           `json:"text"`
	Response        string           `json:"response"`
	Message         *ollamaMessage   `json:"message"`
	ToolCalls       []ollamaToolCall `json:"tool_calls"`
	DoneReason      string           `json:"done_reason"`
	Usage           map[string]int   `json:"usage"`
	EvalCount       int              `json:"eval_count"`
	PromptEvalCount int              `json:"prompt_eval_count"`
}

// NewClient builds a new Ollama client.
func NewClient(endpoint, model string) *Client {
	if endpoint == "" {
		endpoint = "http://localhost:11434"
	}
	return &Client{
		Endpoint: endpoint,
		Model:    model,
		client: &http.Client{
			Timeout: 3 * time.Minute,
		},
	}
}

// Generate implements single prompt completion.
func (c *Client) Generate(ctx context.Context, prompt string, options *framework.LLMOptions) (*framework.LLMResponse, error) {
	payload := map[string]interface{}{
		"model":  c.model(options),
		"prompt": prompt,
		"stream": false,
	}
	c.applyOptions(payload, options)
	return c.doRequest(ctx, "/api/generate", payload)
}

// GenerateStream returns a simple streaming channel.
func (c *Client) GenerateStream(ctx context.Context, prompt string, options *framework.LLMOptions) (<-chan string, error) {
	payload := map[string]interface{}{
		"model":  c.model(options),
		"prompt": prompt,
		"stream": true,
	}
	c.applyOptions(payload, options)
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+"/api/generate", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	ch := make(chan string)
	go func() {
		defer resp.Body.Close()
		defer close(ch)
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			ch <- scanner.Text()
		}
	}()
	return ch, nil
}

// Chat implements chat style conversation.
func (c *Client) Chat(ctx context.Context, messages []framework.Message, options *framework.LLMOptions) (*framework.LLMResponse, error) {
	payload := map[string]interface{}{
		"model":    c.model(options),
		"messages": convertMessages(messages),
		"stream":   false,
	}
	c.applyOptions(payload, options)
	return c.doRequest(ctx, "/api/chat", payload)
}

// ChatWithTools handles tool calling metadata.
func (c *Client) ChatWithTools(ctx context.Context, messages []framework.Message, tools []framework.Tool, options *framework.LLMOptions) (*framework.LLMResponse, error) {
	payload := map[string]interface{}{
		"model":    c.model(options),
		"tools":    convertTools(tools),
		"stream":   false,
		"messages": convertMessages(messages),
	}
	c.applyOptions(payload, options)
	return c.doRequest(ctx, "/api/chat", payload)
}

// SetDebugLogging enables or disables verbose logging for requests/responses.
func (c *Client) SetDebugLogging(enabled bool) {
	c.Debug = enabled
}

func (c *Client) getHTTPClient() *http.Client {
	if c.client != nil {
		return c.client
	}
	c.client = &http.Client{Timeout: 60 * time.Second}
	return c.client
}

func (c *Client) model(options *framework.LLMOptions) string {
	if options != nil && options.Model != "" {
		return options.Model
	}
	if c.Model != "" {
		return c.Model
	}
	return "codellama"
}

func (c *Client) applyOptions(payload map[string]interface{}, options *framework.LLMOptions) {
	if options == nil {
		return
	}
	if options.Temperature != 0 {
		payload["temperature"] = options.Temperature
	}
	if options.MaxTokens != 0 {
		payload["max_tokens"] = options.MaxTokens
	}
	if options.Stop != nil {
		payload["stop"] = options.Stop
	}
	if options.TopP != 0 {
		payload["top_p"] = options.TopP
	}
}

func (c *Client) doRequest(ctx context.Context, path string, payload interface{}) (*framework.LLMResponse, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	c.logPayload(path, body)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Endpoint+path, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.getHTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		detail := strings.TrimSpace(string(msg))
		if detail != "" {
			return nil, fmt.Errorf("ollama error: %s: %s", resp.Status, detail)
		}
		return nil, fmt.Errorf("ollama error: %s", resp.Status)
	}
	responseBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
}
	c.logResponse(path, responseBody)
	return decodeLLMResponse(bytes.NewReader(responseBody))
}

func convertMessages(messages []framework.Message) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		m := map[string]interface{}{
			"role":    msg.Role,
			"content": msg.Content,
		}
		if msg.Name != "" {
			m["name"] = msg.Name
			if msg.Role == "tool" {
				m["tool_name"] = msg.Name
			}
		}
		if msg.ToolCallID != "" {
			m["tool_call_id"] = msg.ToolCallID
		}
		if len(msg.ToolCalls) > 0 {
			calls := make([]map[string]interface{}, 0, len(msg.ToolCalls))
			for _, call := range msg.ToolCalls {
				fn := map[string]interface{}{
					"name": call.Name,
				}
				if len(call.Args) > 0 {
					fn["arguments"] = call.Args
				} else {
					fn["arguments"] = map[string]interface{}{}
				}
				entry := map[string]interface{}{
					"type":     "function",
					"function": fn,
				}
				if call.ID != "" {
					entry["id"] = call.ID
				}
				calls = append(calls, entry)
			}
			m["tool_calls"] = calls
		}
		out = append(out, m)
	}
	return out
}

func convertTools(tools []framework.Tool) []toolDef {
	res := make([]toolDef, 0, len(tools))
	for _, tool := range tools {
		props := make(map[string]interface{})
		var required []string
		for _, param := range tool.Parameters() {
			prop := map[string]interface{}{
				"type":        param.Type,
				"description": param.Description,
			}
			if param.Default != nil {
				prop["default"] = param.Default
			}
			props[param.Name] = prop
			if param.Required {
				required = append(required, param.Name)
			}
		}
		parameters := map[string]interface{}{
			"type":       "object",
			"properties": props,
		}
		if len(required) > 0 {
			parameters["required"] = required
		}
		res = append(res, toolDef{
			Type: "function",
			Function: toolFunction{
				Name:        tool.Name(),
				Description: tool.Description(),
				Parameters:  parameters,
			},
		})
	}
	return res
}

func decodeLLMResponse(body io.Reader) (*framework.LLMResponse, error) {
	var raw ollamaResponse
	if err := json.NewDecoder(body).Decode(&raw); err != nil {
		return nil, err
	}
	resp := &framework.LLMResponse{
		Text:         firstNonEmpty(raw.Text, raw.Response),
		FinishReason: raw.DoneReason,
		Usage:        normalizeUsage(raw),
	}
	if resp.Text == "" && raw.Message != nil {
		resp.Text = raw.Message.Content
	}
	resp.ToolCalls = append(resp.ToolCalls, parseToolCalls(raw.ToolCalls)...)
	if raw.Message != nil {
		resp.ToolCalls = append(resp.ToolCalls, parseToolCalls(raw.Message.ToolCalls)...)
	}
	return resp, nil
}

func parseToolCalls(calls []ollamaToolCall) []framework.ToolCall {
	results := make([]framework.ToolCall, 0, len(calls))
	for _, call := range calls {
		name := call.Name
		args := call.Arguments
		if call.Function.Name != "" {
			name = call.Function.Name
		}
		if len(call.Function.Arguments) > 0 {
			args = call.Function.Arguments
		}
		parsedArgs := parseArguments(args)
		results = append(results, framework.ToolCall{
			ID:   call.ID,
			Name: name,
			Args: parsedArgs,
		})
	}
	return results
}

func parseArguments(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return map[string]interface{}{}
	}
	var obj map[string]interface{}
	if err := json.Unmarshal(raw, &obj); err == nil {
		return obj
	}
	var str string
	if err := json.Unmarshal(raw, &str); err == nil {
		var nested map[string]interface{}
		if err := json.Unmarshal([]byte(str), &nested); err == nil {
			return nested
		}
		return map[string]interface{}{"value": str}
	}
	return map[string]interface{}{"_raw": string(raw)}
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func normalizeUsage(raw ollamaResponse) map[string]int {
	if raw.Usage != nil {
		return raw.Usage
	}
	usage := make(map[string]int)
	if raw.EvalCount > 0 {
		usage["completion_tokens"] = raw.EvalCount
	}
	if raw.PromptEvalCount > 0 {
		usage["prompt_tokens"] = raw.PromptEvalCount
	}
	if len(usage) == 0 {
		return nil
	}
	return usage
}

func (c *Client) logPayload(path string, payload []byte) {
	if !c.Debug {
		return
	}
	c.logf("request %s payload: %s", path, truncate(string(payload), 2048))
}

func (c *Client) logResponse(path string, resp []byte) {
	if !c.Debug {
		return
	}
	c.logf("response %s payload: %s", path, truncate(string(resp), 2048))
}

func (c *Client) logf(format string, args ...interface{}) {
	if !c.Debug {
		return
	}
	log.Printf("[ollama] "+format, args...)
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "...(truncated)"
}
