package proxy

import (
	"fmt"
	"ocs-ai-proxy/internal/model"
)

// BuildSystemPrompt 构建约束模型输出的 System Prompt
func BuildSystemPrompt() string {
	return `你是一个考试答题助手，请严格按照以下规则回答：

1. 仔细阅读题目，给出正确答案
2. 必须使用以下 JSON 格式输出，不要包含任何其他内容：
{
  "answer": "你的答案文本",
  "confidence": 0.95,
  "explanation": "简要解析"
}

规则：
- answer：填写你选择的答案内容
- confidence：0-1 之间的浮点数，表示你对答案的把握程度
- explanation：简要说明选择该答案的原因
- 不要输出任何 markdown 标记或额外文字
- 如果题目为多选题，answer 字段用逗号分隔多个选项`
}

// BuildRequestBody 构建发送给本地大模型的完整请求
func BuildRequestBody(originalReq *model.ChatRequest) *model.ChatRequest {
	systemMsg := model.ChatMessage{
		Role:    "system",
		Content: BuildSystemPrompt(),
	}

	messages := make([]model.ChatMessage, 0, len(originalReq.Messages)+1)
	messages = append(messages, systemMsg)
	messages = append(messages, originalReq.Messages...)

	return &model.ChatRequest{
		Model:       originalReq.Model,
		Messages:    messages,
		Temperature: 0.1,
		MaxTokens:   512,
		Stream:      false,
	}
}

// BuildFallbackResponse 在模型不可用时构造降级响应
func BuildFallbackResponse(errMsg string) *model.ChatResponse {
	return &model.ChatResponse{
		ID:     "fallback-0000",
		Object: "chat.completion",
		Model:  "fallback",
		Choices: []model.ChatChoice{
			{
				Index:        0,
				FinishReason: "error",
				Message: model.ChatMessage{
					Role:    "assistant",
					Content: fmt.Sprintf(`{"answer":"","confidence":0,"explanation":"模型不可用: %s"}`, errMsg),
				},
			},
		},
	}
}
