package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// Client 是一个 OpenAI 兼容的 Chat Completions 客户端。
type Client struct {
	http *http.Client
}

func NewClient() *Client {
	return &Client{http: &http.Client{Timeout: 5 * time.Minute}}
}

// chatRequest 是发送给 /chat/completions 的请求体。
type chatRequest struct {
	Model          string          `json:"model"`
	Messages       []Message       `json:"messages"`
	Temperature    float64         `json:"temperature"`
	TopP           float64         `json:"top_p"`
	MaxTokens      int             `json:"max_tokens,omitempty"`
	Stream         bool            `json:"stream"`
	ResponseFormat *ResponseFormat `json:"response_format,omitempty"`
}

// streamChunk 是流式响应中单个 SSE 数据块的结构。
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content string `json:"content"`
		} `json:"delta"`
		FinishReason *string `json:"finish_reason"`
	} `json:"choices"`
}

// completionResp 是非流式响应的结构。
type completionResp struct {
	Choices []struct {
		Message Message `json:"message"`
	} `json:"choices"`
}

// Stream 以流式方式请求模型，每个增量通过 onDelta 回调返回，最终返回完整文本。
// ctx 取消时请求随之中止。
func (c *Client) Stream(ctx context.Context, p Params, messages []Message, onDelta func(string)) (string, error) {
	body := chatRequest{
		Model:          p.Model,
		Messages:       messages,
		Temperature:    p.Temperature,
		TopP:           p.TopP,
		MaxTokens:      p.MaxTokens,
		Stream:         true,
		ResponseFormat: p.ResponseFormat,
	}
	resp, err := c.do(ctx, p, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", c.httpError(resp)
	}

	var full strings.Builder
	reader := bufio.NewReader(resp.Body)
	for {
		line, err := reader.ReadString('\n')
		if len(line) > 0 {
			data, ok := parseSSE(line)
			if ok {
				if data == "[DONE]" {
					break
				}
				var chunk streamChunk
				if json.Unmarshal([]byte(data), &chunk) == nil {
					for _, ch := range chunk.Choices {
						if ch.Delta.Content != "" {
							full.WriteString(ch.Delta.Content)
							if onDelta != nil {
								onDelta(ch.Delta.Content)
							}
						}
					}
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return full.String(), err
		}
	}
	return full.String(), nil
}

// Complete 以非流式方式请求模型并返回完整文本。
func (c *Client) Complete(ctx context.Context, p Params, messages []Message) (string, error) {
	body := chatRequest{
		Model:          p.Model,
		Messages:       messages,
		Temperature:    p.Temperature,
		TopP:           p.TopP,
		MaxTokens:      p.MaxTokens,
		Stream:         false,
		ResponseFormat: p.ResponseFormat,
	}
	resp, err := c.do(ctx, p, body)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", c.httpError(resp)
	}
	var out completionResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("模型未返回任何 choices")
	}
	return out.Choices[0].Message.Content, nil
}

func (c *Client) do(ctx context.Context, p Params, body chatRequest) (*http.Response, error) {
	if p.APIKey == "" {
		return nil, fmt.Errorf("未配置 LLM API Key")
	}
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	url := strings.TrimRight(p.BaseURL, "/") + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+p.APIKey)
	if body.Stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	return c.http.Do(req)
}

func (c *Client) httpError(resp *http.Response) error {
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	return fmt.Errorf("LLM 请求失败 (%d): %s", resp.StatusCode, strings.TrimSpace(string(b)))
}

// parseSSE 解析一行 SSE，返回 data 字段内容。
func parseSSE(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, ":") {
		return "", false
	}
	if strings.HasPrefix(line, "data:") {
		return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
	}
	return "", false
}
