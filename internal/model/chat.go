package model

// ChatMessage OpenAI ChatCompletion 格式的消息体
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest OpenAI ChatCompletion 格式的请求体
type ChatRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

// ChatChoice OpenAI ChatCompletion 格式的选项
type ChatChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatResponse OpenAI ChatCompletion 格式的响应
type ChatResponse struct {
	ID      string       `json:"id"`
	Object  string       `json:"object"`
	Created int64        `json:"created"`
	Model   string       `json:"model"`
	Choices []ChatChoice `json:"choices"`
}

// AnswerResult 从模型输出中解析出的结构化答案
type AnswerResult struct {
	Answer      string  `json:"answer"`
	Confidence  float64 `json:"confidence"`
	Explanation string  `json:"explanation"`
}
