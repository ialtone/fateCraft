// Package llm 是 OpenAI 兼容 Chat Completions 的 HTTP 客户端，
// 只承载「请求侧」类型（Message/Params/ResponseFormat/JSONSchema）与
// Stream/Complete 调用；模型输出的信封协议见 internal/protocol 包。
package llm

// Message 是一条 Chat Completion 消息。
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Params 是一次请求的模型参数。
type Params struct {
	BaseURL        string
	APIKey         string
	Model          string
	Temperature    float64
	TopP           float64
	MaxTokens      int
	ResponseFormat *ResponseFormat // 强制 JSON 输出
}

// ResponseFormat 用于强制模型输出 JSON 格式。
type ResponseFormat struct {
	Type       string      `json:"type"` // "json_object" 或 "json_schema"
	JSONSchema *JSONSchema `json:"json_schema,omitempty"`
}

// JSONSchema 定义 JSON Schema 约束（OpenAI Structured Outputs）。
type JSONSchema struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Schema      map[string]any `json:"schema"`
	Strict      bool           `json:"strict"`
}
