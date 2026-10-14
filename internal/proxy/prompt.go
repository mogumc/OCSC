package proxy

import (
	"fmt"
	"strings"

	"ocs-ai-proxy/internal/model"
)

// systemPrompt 与参考实现保持一致：用最短的话把模型钉在「只输出 JSON」上。
const systemPrompt = "你是一个只输出 JSON 的专业做题助手。"

// buildMessages 构造发给上游模型的对话消息。
// images 为空时使用纯文本 content，否则组装成多模态 content。
func buildMessages(req *model.SearchRequest, images []string) []model.Message {
	return []model.Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: buildUserContent(req, images)},
	}
}

// buildUserContent 生成用户消息内容。
// 有图片时返回 []model.ContentPart，让支持视觉的模型看到题图。
func buildUserContent(req *model.SearchRequest, images []string) any {
	text := buildPrompt(req)
	if len(images) == 0 {
		return text
	}

	parts := make([]model.ContentPart, 0, len(images)+1)
	parts = append(parts, model.ContentPart{Type: "text", Text: text})
	for _, img := range images {
		parts = append(parts, model.ContentPart{
			Type:     "image_url",
			ImageURL: &model.ImageURL{URL: img},
		})
	}
	return parts
}

// buildPrompt 按题型拼接题目描述与输出约束。
func buildPrompt(req *model.SearchRequest) string {
	var b strings.Builder

	b.WriteString("你是一个专业的学术助教。请仔细阅读题目和选项，选出最正确的答案。\n\n")
	fmt.Fprintf(&b, "题目: %s\n", req.Title)
	if req.Options != "" {
		fmt.Fprintf(&b, "选项: \n%s\n", req.Options)
	}
	fmt.Fprintf(&b, "题目类型: %s\n", model.TypeLabel(req.Type))
	if tip := typeInstruction(req.Type); tip != "" {
		fmt.Fprintf(&b, "%s\n", tip)
	}

	b.WriteString(`
请严格遵守以下规则：
1. 仅输出一个合法的 JSON 对象，不要包含 Markdown 标记或额外文字。
2. JSON 格式必须如下：
{
    "answer": "这里填最准确的答案；多选题用 # 号分隔多个选项",
    "analysis": "这里填写简短的解析"
}`)

	return b.String()
}

// typeInstruction 返回各题型额外的作答约束。
func typeInstruction(qType string) string {
	switch qType {
	case model.TypeMultiple:
		return "重要提示：这是一道【多选题】，请仔细分析所有选项，选出全部正确答案，并用 # 号分隔（例如：A#B#C），不要漏选。"
	case model.TypeCompletion:
		return "重要提示：这是一道【填空题】，请直接输出最准确的填空内容，不要输出选项字母。"
	case model.TypeJudgement:
		return "重要提示：这是一道【判断题】，请只回答「对」或「错」。"
	default:
		return ""
	}
}
