package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"ocs-ai-proxy/internal/config"
	"ocs-ai-proxy/internal/model"
	"strings"
	"time"
)

// LLMProxy 本地大模型代理
type LLMProxy struct {
	client *http.Client
	config *config.Config
}

// NewLLMProxy 创建代理实例
func NewLLMProxy(cfg *config.Config) *LLMProxy {
	return &LLMProxy{
		client: &http.Client{
			Timeout: time.Duration(cfg.TimeoutSec) * time.Second,
		},
		config: cfg,
	}
}

// Chat 发送对话请求到本地大模型并返回 OpenAI 兼容响应
func (p *LLMProxy) Chat(originalReq *model.ChatRequest) (*model.ChatResponse, error) {
	fullReq := BuildRequestBody(originalReq)
	fullReq.Model = p.config.LLMModel

	reqBody, err := json.Marshal(fullReq)
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	httpReq, err := http.NewRequest(http.MethodPost, p.config.LLMEndpoint+"/chat/completions", bytes.NewReader(reqBody))
	if err != nil {
		return nil, fmt.Errorf("构建 HTTP 请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	httpResp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求本地模型失败: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("读取响应失败: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("模型返回非 200 状态码: %d, body: %s", httpResp.StatusCode, string(respBody))
	}

	var llmResp model.ChatResponse
	if err := json.Unmarshal(respBody, &llmResp); err != nil {
		return nil, fmt.Errorf("解析模型响应失败: %w", err)
	}

	return &llmResp, nil
}

// ExtractAnswer 从模型响应中提取结构化答案
func (p *LLMProxy) ExtractAnswer(resp *model.ChatResponse) (*model.AnswerResult, error) {
	if len(resp.Choices) == 0 {
		return nil, fmt.Errorf("响应中无 choices 字段")
	}

	content := resp.Choices[0].Message.Content
	return parseAnswerContent(content)
}

// parseAnswerContent 从模型输出文本中解析 AnswerResult
func parseAnswerContent(content string) (*model.AnswerResult, error) {
	jsonStr := extractJSONBlock(content)
	if jsonStr == "" {
		jsonStr = content
	}

	var result model.AnswerResult
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		return nil, fmt.Errorf("解析答案 JSON 失败: %w, 原始内容: %s", err, truncateStr(content, 200))
	}

	if result.Answer == "" {
		return nil, fmt.Errorf("答案为空，解析内容: %s", truncateStr(content, 200))
	}

	return &result, nil
}

// extractJSONBlock 从文本中提取 JSON 对象块
func extractJSONBlock(text string) string {
	// 尝试 
	start := strings.Index(text, "")
		if end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	// 尝试 
	start = strings.Index(text, "")
		if end != -1 {
			return strings.TrimSpace(text[start : start+end])
		}
	}

	// 直接查找 { } 包裹的 JSON
	braceStart := strings.Index(text, "{")
	if braceStart != -1 {
		braceEnd := strings.LastIndex(text, "}")
		if braceEnd > braceStart {
			return text[braceStart : braceEnd+1]
		}
	}

	return ""
}

// ChatWithRetry 带重试的对话
func (p *LLMProxy) ChatWithRetry(originalReq *model.ChatRequest) (*model.ChatResponse, *model.AnswerResult, error) {
	var lastErr error

	for attempt := 0; attempt <= p.config.MaxRetries; attempt++ {
		resp, err := p.Chat(originalReq)
		if err != nil {
			lastErr = err
			continue
		}

		answer, err := p.ExtractAnswer(resp)
		if err != nil {
			lastErr = err
			continue
		}

		return resp, answer, nil
	}

	return nil, nil, fmt.Errorf("重试 %d 次后仍失败: %w", p.config.MaxRetries, lastErr)
}

func truncateStr(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
