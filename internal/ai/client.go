package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"
)

type Error struct {
	Code, Message string
	Retry         bool
	After         time.Duration
}

func (e *Error) Error() string { return e.Code }
func Invalid() error {
	return &Error{Code: "AI_OUTPUT_INVALID", Message: "模型输出格式无效，请重试"}
}

type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}
type Client struct {
	URL, Key, Model string
	Budget, Output  int
	HTTP            *http.Client
}

func (c *Client) Check(messages []Message) error {
	if c.Key == "" || c.Model == "" {
		return &Error{Code: "AI_NOT_CONFIGURED", Message: "管理员尚未配置模型密钥与模型名称"}
	}
	b, _ := json.Marshal(messages)
	// Byte count is a conservative estimate, not the provider's tokenizer or billing.
	if len(b)+c.Output+2048 > c.Budget {
		return &Error{Code: "CONTEXT_BUDGET_EXCEEDED", Message: "完整字幕和对话超出当前模型预算，未调用模型"}
	}
	return nil
}
func (c *Client) Complete(ctx context.Context, messages []Message, jsonOutput bool) (string, error) {
	if err := c.Check(messages); err != nil {
		return "", err
	}
	body := map[string]any{"model": c.Model, "messages": messages, "max_tokens": c.Output, "stream": false}
	if jsonOutput {
		body["response_format"] = map[string]string{"type": "json_object"}
	}
	b, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", strings.TrimRight(c.URL, "/")+"/chat/completions", bytes.NewReader(b))
	if err != nil {
		return "", &Error{Code: "AI_CONFIGURATION_INVALID", Message: "模型服务配置无效"}
	}
	req.Header.Set("Authorization", "Bearer "+c.Key)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) {
			return "", err
		}
		return "", &Error{Code: "AI_PROVIDER_UNAVAILABLE", Message: "模型服务暂时不可用", Retry: true, After: 5 * time.Second}
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		retry := res.StatusCode == 408 || res.StatusCode == 429 || res.StatusCode >= 500
		after := 5 * time.Second
		if seconds, err := time.ParseDuration(res.Header.Get("Retry-After") + "s"); err == nil && seconds > after {
			after = min(seconds, 2*time.Minute)
		}
		code := "AI_PROVIDER_REJECTED"
		if retry {
			code = "AI_PROVIDER_UNAVAILABLE"
		}
		return "", &Error{Code: code, Message: "模型服务未能完成请求，请检查配置或稍后重试", Retry: retry, After: after}
	}
	data, err := io.ReadAll(io.LimitReader(res.Body, (2<<20)+1))
	if err != nil {
		return "", &Error{Code: "AI_PROVIDER_UNAVAILABLE", Message: "模型响应中断", Retry: true, After: 5 * time.Second}
	}
	if len(data) > 2<<20 {
		return "", Invalid()
	}
	var result struct {
		Choices []struct {
			Finish  string  `json:"finish_reason"`
			Message Message `json:"message"`
		} `json:"choices"`
	}
	if json.Unmarshal(data, &result) != nil || len(result.Choices) != 1 || result.Choices[0].Finish != "stop" || strings.TrimSpace(result.Choices[0].Message.Content) == "" {
		return "", Invalid()
	}
	return strings.TrimSpace(result.Choices[0].Message.Content), nil
}
