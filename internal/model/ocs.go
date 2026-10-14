package model

import (
	"bytes"
	"encoding/json"
	"strings"
)

// OCS 网课助手使用的题型标识。
const (
	TypeSingle     = "single"
	TypeMultiple   = "multiple"
	TypeJudgement  = "judgement"
	TypeCompletion = "completion"
)

// SearchRequest 是 OCS 题库协议（自定义题库）的请求体。
//
// Images / SuggestionTitle / SuggestionOptions 由 OCS「图片题优化」提供
// （需要 OCS 5.1 及以上并在设置里开启）：开启后 OCS 会把题干图片转成 Base64
// 一并上传，原题的 title / options 不会被修改。
//
// SkipCache 是本项目的扩展字段：置为 true 时跳过缓存读取，强制重新问一次模型。
type SearchRequest struct {
	Title             string `json:"title"`
	Options           string `json:"options"`
	Type              string `json:"type"`
	Images            Images `json:"images,omitempty"`
	SuggestionTitle   string `json:"suggestion_title,omitempty"`
	SuggestionOptions string `json:"suggestion_options,omitempty"`
	SkipCache         bool   `json:"skip_cache,omitempty"`
}

// Images 兼容下游传图片的两种写法：
//   - JSON 数组（推荐：配置里用字段解析器返回 env.images）
//   - 字符串（把 "${images}" 直接写进配置时，OCS 会拼成字符串传过来）
//
// 之所以要容忍字符串，是因为写法不对时如果直接反序列化失败，整个请求会 400，
// 连纯文字题都会一起挂掉。声明成具名类型只是为了自定义反序列化，
// 底层仍是 []string，可以直接当切片用。
type Images []string

// UnmarshalJSON 实现「数组或字符串都能接住」。
func (im *Images) UnmarshalJSON(data []byte) error {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 || string(trimmed) == "null" {
		*im = nil
		return nil
	}

	if trimmed[0] == '[' {
		var list []string
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return err
		}
		*im = list
		return nil
	}

	// 字符串形式：可能是单张图，也可能是按换行拼起来的多张
	var raw string
	if err := json.Unmarshal(trimmed, &raw); err != nil {
		return err
	}

	var list []string
	for _, part := range strings.Split(raw, "\n") {
		if part = strings.TrimSpace(part); part != "" {
			list = append(list, part)
		}
	}
	*im = list
	return nil
}

// SearchResponse 是 OCS 题库协议的响应体。
//
// code=1 表示查到答案，OCS 会取 question/answer；
// code=0 表示本次作答失败，OCS 会读取 msg 并继续尝试后续题库。
type SearchResponse struct {
	Code     int    `json:"code"`
	Question string `json:"question,omitempty"`
	Answer   string `json:"answer,omitempty"`
	Analysis string `json:"analysis,omitempty"`
	Msg      string `json:"msg,omitempty"`
}

// AnswerResult 是模型输出被解析后的结构化答案。
type AnswerResult struct {
	Answer   string `json:"answer"`
	Analysis string `json:"analysis"`
}

// TypeLabel 返回题型的中文名，用于日志与提示词。
func TypeLabel(t string) string {
	switch t {
	case TypeSingle:
		return "单选题"
	case TypeMultiple:
		return "多选题"
	case TypeJudgement:
		return "判断题"
	case TypeCompletion:
		return "填空题"
	case "":
		return "未知类型"
	default:
		return t
	}
}

// —— 以下为上游大模型（OpenAI 兼容接口）的通信结构 ——

// Message 的 Content 支持两种形态：
// 纯文本时为 string，携带图片时为 []ContentPart。
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// ContentPart 是多模态消息中的一个片段。
type ContentPart struct {
	Type     string    `json:"type"`
	Text     string    `json:"text,omitempty"`
	ImageURL *ImageURL `json:"image_url,omitempty"`
}

// ImageURL 是图片引用，支持 data URI 与 http(s) 链接。
type ImageURL struct {
	URL string `json:"url"`
}

// CompletionRequest 是发送给上游模型的请求体。
type CompletionRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream"`
}

// CompletionResponse 是上游返回的响应体，只保留用得到的字段。
//
// 推理模型会把思维链单独放在 reasoning_content 里（DeepSeek-R1 等官方 API 的做法），
// 这里只读 content，推理字段一律不参与答案。
type CompletionResponse struct {
	Choices []struct {
		Message struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"message"`
	} `json:"choices"`
}
