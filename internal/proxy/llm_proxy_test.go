package proxy

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"ocs-ai-proxy/internal/config"
	"ocs-ai-proxy/internal/model"
)

// testConfig 返回一份与本地无关的测试配置，供不需要真正发起请求的用例使用。
func testConfig(enableVision bool) config.Config {
	return config.Config{
		ListenAddr:   ":8080",
		BaseURL:      "http://127.0.0.1:1/v1",
		Model:        "mock-model",
		TimeoutSec:   1,
		MaxRetries:   0,
		EnableVision: enableVision,
		AuthHeader:   "Authorization",
	}
}

func TestSniffImageMime(t *testing.T) {
	// 用真实文件头 + 填充字节构造一张「图片」，再整体做标准 base64，
	// 避免手写 base64 字符串出错
	cases := []struct {
		name string
		head []byte
		want string
	}{
		{"PNG", []byte{0x89, 'P', 'N', 'G', 0x0D, 0x0A, 0x1A, 0x0A}, "image/png"},
		{"JPEG", []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 0x4A, 0x46}, "image/jpeg"},
		{"GIF", []byte("GIF89a"), "image/gif"},
		{"WebP", []byte("RIFF\x00\x00\x00\x00WEBP"), "image/webp"},
		{"BMP", []byte("BM\x00\x00"), "image/bmp"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			payload := append(append([]byte{}, c.head...), bytes.Repeat([]byte{0}, 1024)...)
			if got := sniffImageMime(base64.StdEncoding.EncodeToString(payload)); got != c.want {
				t.Fatalf("sniffImageMime() = %q，期望 %q", got, c.want)
			}
		})
	}

	t.Run("认不出的格式退回 PNG", func(t *testing.T) {
		if got := sniffImageMime("AAAAAAAA"); got != defaultImageMime {
			t.Fatalf("应当退回 %q，实际 %q", defaultImageMime, got)
		}
	})

	t.Run("不是合法 base64 时退回 PNG", func(t *testing.T) {
		if got := sniffImageMime("这不是 base64!!!"); got != defaultImageMime {
			t.Fatalf("应当退回 %q，实际 %q", defaultImageMime, got)
		}
	})

	t.Run("过短的数据退回 PNG", func(t *testing.T) {
		if got := sniffImageMime("AA"); got != defaultImageMime {
			t.Fatalf("应当退回 %q，实际 %q", defaultImageMime, got)
		}
	})
}

func TestCleanContent(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{
			name: "剥掉 json 代码块围栏",
			in:   "```json\n{\"answer\":\"B\",\"analysis\":\"1+1=2\"}\n```",
			want: `{"answer":"B","analysis":"1+1=2"}`,
		},
		{
			name: "去掉 think 推理段",
			in:   "<think>先算 1+1</think>\n{\"answer\":\"B\",\"analysis\":\"1+1=2\"}",
			want: `{"answer":"B","analysis":"1+1=2"}`,
		},
		{
			name: "多行 think 也能去掉",
			in:   "```json\n<think>\n第一步\n第二步\n</think>\n{\"answer\":\"B\"}\n```",
			want: `{"answer":"B"}`,
		},
		{
			name: "前后有多余解释文字时截取 JSON",
			in:   "好的，答案是：{\"answer\":\"B\",\"analysis\":\"1+1=2\"} 希望有帮助",
			want: `{"answer":"B","analysis":"1+1=2"}`,
		},
		{
			name: "嵌套对象取最外层",
			in:   `{"answer":"B","extra":{"k":1}}`,
			want: `{"answer":"B","extra":{"k":1}}`,
		},
		{
			name: "没有 JSON 时原样返回",
			in:   "模型今天不想说话",
			want: "模型今天不想说话",
		},
		{
			name: "思维链被截断（未闭合）时整段丢弃",
			in:   `<think>我在推理，中间提到了 {"answer":"这是推理里的假答案"}`,
			want: "",
		},
		{
			name: "未闭合思维链之后的真实答案也一并丢弃，交由上层重试",
			in:   `<think>推理没写完……`,
			want: "",
		},
		{
			name: "闭合思维链之后的答案正常保留",
			in:   `<think>推理里出现 {"answer":"假"} 干扰项</think>{"answer":"真"}`,
			want: `{"answer":"真"}`,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanContent(c.in); got != c.want {
				t.Fatalf("cleanContent() = %q，期望 %q", got, c.want)
			}
		})
	}
}

func TestParseAnswer(t *testing.T) {
	t.Run("正常解析并去除空白", func(t *testing.T) {
		result, err := parseAnswer("```json\n{\"answer\":\" B \",\"analysis\":\" 解析 \"}\n```")
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if result.Answer != "B" || result.Analysis != "解析" {
			t.Fatalf("解析结果 = %+v", result)
		}
	})

	t.Run("多选题用井号分隔", func(t *testing.T) {
		result, err := parseAnswer(`{"answer":"A#C#D","analysis":"略"}`)
		if err != nil {
			t.Fatalf("不应报错: %v", err)
		}
		if result.Answer != "A#C#D" {
			t.Fatalf("answer = %q", result.Answer)
		}
	})

	t.Run("空输出报错", func(t *testing.T) {
		if _, err := parseAnswer("   "); err == nil {
			t.Fatal("空输出应当报错")
		}
	})

	t.Run("非法 JSON 报错", func(t *testing.T) {
		if _, err := parseAnswer("这不是 JSON"); err == nil {
			t.Fatal("非法 JSON 应当报错")
		}
	})

	t.Run("answer 缺失报错", func(t *testing.T) {
		if _, err := parseAnswer(`{"analysis":"只有解析"}`); err == nil {
			t.Fatal("answer 为空应当报错")
		}
	})

	t.Run("只输出了思维链时报错而不是返回空答案", func(t *testing.T) {
		_, err := parseAnswer("<think>想了半天没想出来</think>")
		if err == nil {
			t.Fatal("只输出思维链时应当报错，交给上层重试")
		}
		if !strings.Contains(err.Error(), "思维链") {
			t.Fatalf("报错信息应当能指出是思维链问题，实际: %v", err)
		}
	})
}

func TestContentFromCompletionReasoningOnly(t *testing.T) {
	// 推理模型的思维链放在独立字段里，content 为空 —— 说明答案被截断了
	body := `{"choices":[{"message":{"content":"","reasoning_content":"我一直在推理，然后就被截断了"}}]}`

	_, err := contentFromCompletion([]byte(body))
	if err == nil {
		t.Fatal("只返回 reasoning_content 时应当报错")
	}
	if !errors.Is(err, errReasoningOnly) {
		t.Fatalf("应当返回可定位的 errReasoningOnly，实际: %v", err)
	}
}

func TestContentFromCompletionIgnoresReasoning(t *testing.T) {
	// 有正常 content 时，reasoning_content 一律不参与
	body := `{"choices":[{"message":{"content":"{\"answer\":\"B\"}","reasoning_content":"推理过程"}}]}`

	content, err := contentFromCompletion([]byte(body))
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if content != `{"answer":"B"}` {
		t.Fatalf("content = %q，推理内容不应混入", content)
	}
}

func TestReadAnswerContentStreamingReasoningOnly(t *testing.T) {
	cfg := testConfig(false)
	cfg.Stream = true
	p := NewLLMProxy(&cfg)

	// 全程只吐 reasoning_content，没有任何 content
	stream := sseChunk("", "第一段推理") +
		sseChunk("", "第二段推理") +
		"data: [DONE]\n\n"

	_, err := p.readAnswerContent(strings.NewReader(stream), nil)
	if err == nil {
		t.Fatal("只有推理内容时应当报错")
	}
	if !errors.Is(err, errReasoningOnly) {
		t.Fatalf("应当返回 errReasoningOnly，实际: %v", err)
	}
}

func TestNormalizeImageRef(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
		ok   bool
	}{
		{"data URI 原样保留", "data:image/jpeg;base64,/9j/4AAQ", "data:image/jpeg;base64,/9j/4AAQ", true},
		{"https 链接原样保留", "https://example.com/q.png", "https://example.com/q.png", true},
		{"http 链接原样保留", "http://example.com/q.png", "http://example.com/q.png", true},
		{"裸 base64（PNG）按文件头补 MIME", "iVBORw0KGgoAAA", "data:image/png;base64,iVBORw0KGgoAAA", true},
		{"裸 base64（JPEG）按文件头补 MIME", "/9j/4AAQ", "data:image/jpeg;base64,/9j/4AAQ", true},
		{"裸 base64（GIF）按文件头补 MIME", "R0lGODlh", "data:image/gif;base64,R0lGODlh", true},
		{"带空格的裸 base64", "  iVBORw0KGgoAAA  ", "data:image/png;base64,iVBORw0KGgoAAA", true},
		{"空字符串丢弃", "   ", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := normalizeImageRef(c.in)
			if ok != c.ok || got != c.want {
				t.Fatalf("normalizeImageRef(%q) = (%q, %v)，期望 (%q, %v)", c.in, got, ok, c.want, c.ok)
			}
		})
	}
}

func TestBuildUserContent(t *testing.T) {
	req := &model.SearchRequest{Title: "看图作答", Type: model.TypeMultiple}

	t.Run("无图片时是纯文本", func(t *testing.T) {
		content := buildUserContent(req, nil)
		text, ok := content.(string)
		if !ok {
			t.Fatalf("无图片时 content 应为 string，实际 %T", content)
		}
		if !strings.Contains(text, "多选题") || !strings.Contains(text, "#") {
			t.Fatalf("多选题提示词缺失，实际内容:\n%s", text)
		}
	})

	t.Run("有图片时是多模态片段", func(t *testing.T) {
		content := buildUserContent(req, []string{"data:image/png;base64,AAA", "https://example.com/b.png"})
		parts, ok := content.([]model.ContentPart)
		if !ok {
			t.Fatalf("有图片时 content 应为 []ContentPart，实际 %T", content)
		}
		if len(parts) != 3 {
			t.Fatalf("期望 1 段文本 + 2 段图片，实际 %d 段", len(parts))
		}
		if parts[0].Type != "text" {
			t.Errorf("首段应为 text，实际 %q", parts[0].Type)
		}
		for i, part := range parts[1:] {
			if part.Type != "image_url" || part.ImageURL == nil {
				t.Fatalf("第 %d 段图片结构不正确: %+v", i+1, part)
			}
		}
	})

	t.Run("序列化后的 JSON 结构符合 OpenAI 多模态格式", func(t *testing.T) {
		payload, err := json.Marshal(buildUserContent(req, []string{"https://example.com/a.png"}))
		if err != nil {
			t.Fatalf("序列化失败: %v", err)
		}
		var parts []map[string]any
		if err := json.Unmarshal(payload, &parts); err != nil {
			t.Fatalf("反序列化失败: %v", err)
		}
		if parts[1]["type"] != "image_url" {
			t.Fatalf("第二段 type 应为 image_url: %s", payload)
		}
		imageURL, ok := parts[1]["image_url"].(map[string]any)
		if !ok || imageURL["url"] != "https://example.com/a.png" {
			t.Fatalf("image_url.url 不正确: %s", payload)
		}
	})
}

func TestResolveImagesHonoursSwitch(t *testing.T) {
	newProxy := func(enableVision bool) *LLMProxy {
		cfg := testConfig(enableVision)
		return NewLLMProxy(&cfg)
	}

	t.Run("开关关闭时忽略图片", func(t *testing.T) {
		if got := newProxy(false).resolveImages([]string{"data:image/png;base64,AAA"}); got != nil {
			t.Fatalf("开关关闭时应返回 nil，实际 %v", got)
		}
	})

	t.Run("开关开启时保留图片", func(t *testing.T) {
		got := newProxy(true).resolveImages([]string{"data:image/png;base64,AAA"})
		if len(got) != 1 {
			t.Fatalf("期望 1 张图片，实际 %d", len(got))
		}
	})

	t.Run("超过上限时截断", func(t *testing.T) {
		raw := make([]string, maxImages+3)
		for i := range raw {
			raw[i] = "data:image/png;base64,AAA"
		}
		if got := newProxy(true).resolveImages(raw); len(got) != maxImages {
			t.Fatalf("期望截断到 %d 张，实际 %d", maxImages, len(got))
		}
	})

	t.Run("无图片时返回 nil", func(t *testing.T) {
		if got := newProxy(true).resolveImages(nil); got != nil {
			t.Fatalf("无图片时应返回 nil，实际 %v", got)
		}
	})
}

func TestTruncateIsRuneSafe(t *testing.T) {
	// 直接按字节切片会把多字节字符切碎，这里确认按字符处理
	got := truncate("中文测试内容", 3)
	if got != "中文测..." {
		t.Fatalf("truncate() = %q", got)
	}
	if got := truncate("abc", 10); got != "abc" {
		t.Fatalf("短字符串不应被截断，实际 %q", got)
	}
}

// sseChunk 构造一个流式分片。content 为空时只带 reasoning_content，
// 用于验证推理字段会被忽略。
func sseChunk(content, reasoning string) string {
	delta := map[string]any{}
	if content != "" {
		delta["content"] = content
	}
	if reasoning != "" {
		delta["reasoning_content"] = reasoning
	}
	payload, _ := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"delta": delta}},
	})
	return "data: " + string(payload) + "\n\n"
}

func TestReadSSEAssemblesDeltas(t *testing.T) {
	stream := sseChunk("```json\n", "") +
		sseChunk("<think>先算 1+1</think>\n", "") +
		sseChunk(`{"answer": "B", `, "") +
		sseChunk(`"analysis": "1+1=2"}`, "") +
		sseChunk("\n```", "") +
		sseChunk("", "这段是推理内容，应当被忽略") +
		"data: [DONE]\n\n"

	got, err := readSSE(strings.NewReader(stream), nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !got.IsSSE {
		t.Fatal("应当被识别为 SSE 流")
	}
	if !got.HasReasoning {
		t.Error("上游返回了 reasoning_content，应当被标记出来")
	}
	if strings.Contains(got.Content, "推理内容") {
		t.Fatalf("reasoning_content 不应进入答案文本: %q", got.Content)
	}

	// 拼接结果要能被原有的清洗 + 解析链路正确处理
	result, err := parseAnswer(got.Content)
	if err != nil {
		t.Fatalf("拼接后的内容应当可解析: %v，原文 %q", err, got.Content)
	}
	if result.Answer != "B" || result.Analysis != "1+1=2" {
		t.Fatalf("解析结果 = %+v", result)
	}
}

func TestReadSSEStopsAtDone(t *testing.T) {
	stream := sseChunk(`{"answer":"B"}`, "") +
		"data: [DONE]\n\n" +
		sseChunk("这段在 DONE 之后，不应被拼接", "")

	got, err := readSSE(strings.NewReader(stream), nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if !got.IsSSE || strings.Contains(got.Content, "DONE 之后") {
		t.Fatalf("遇到 [DONE] 应当立即结束，实际 content=%q isSSE=%v", got.Content, got.IsSSE)
	}
}

func TestReadSSEReportsActivity(t *testing.T) {
	stream := sseChunk("a", "") + sseChunk("b", "") + "data: [DONE]\n\n"

	touches := 0
	if _, err := readSSE(strings.NewReader(stream), func() { touches++ }); err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if touches == 0 {
		t.Fatal("读到数据时必须回调续期，否则流式长回答会被误判为空闲超时")
	}
}

func TestReadSSEWithoutDataLines(t *testing.T) {
	// 上游忽略了 stream 参数，直接返回整体 JSON
	body := `{"choices":[{"message":{"content":"{\"answer\":\"B\"}"}}]}`

	got, err := readSSE(strings.NewReader(body), nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if got.IsSSE {
		t.Fatal("没有 data: 行时不应判定为 SSE")
	}
	if got.Content != body {
		t.Fatalf("原始文本应被完整保留，实际 %q", got.Content)
	}
}

func TestReadAnswerContentFallsBackWhenUpstreamIgnoresStream(t *testing.T) {
	cfg := testConfig(false)
	cfg.Stream = true
	p := NewLLMProxy(&cfg)

	// 上游忽略了 stream 参数，返回整体 JSON，其中 content 是一段带围栏的脏输出
	dirty := "```json\n{\"answer\":\"B\",\"analysis\":\"1+1=2\"}\n```"
	body, err := json.Marshal(map[string]any{
		"choices": []any{map[string]any{"message": map[string]any{"content": dirty}}},
	})
	if err != nil {
		t.Fatalf("构造测试数据失败: %v", err)
	}

	content, err := p.readAnswerContent(strings.NewReader(string(body)), nil)
	if err != nil {
		t.Fatalf("上游返回整体 JSON 时应当退化解析，而不是报错: %v", err)
	}
	if content != dirty {
		t.Fatalf("取出的 content 不正确: %q", content)
	}

	result, err := parseAnswer(content)
	if err != nil || result.Answer != "B" {
		t.Fatalf("退化路径应当继续走通清洗与解析，实际 %+v err=%v", result, err)
	}
}

func TestReadAnswerContentNonStream(t *testing.T) {
	cfg := testConfig(false)
	cfg.Stream = false
	p := NewLLMProxy(&cfg)

	body := `{"choices":[{"message":{"content":"{\"answer\":\"A#C\"}"}}]}`
	content, err := p.readAnswerContent(strings.NewReader(body), nil)
	if err != nil {
		t.Fatalf("不应报错: %v", err)
	}
	if content != `{"answer":"A#C"}` {
		t.Fatalf("content = %q", content)
	}
}

func TestContentFromCompletionErrors(t *testing.T) {
	if _, err := contentFromCompletion([]byte("not json")); err == nil {
		t.Fatal("非法 JSON 应当报错")
	}
	if _, err := contentFromCompletion([]byte(`{"choices":[]}`)); err == nil {
		t.Fatal("没有 choices 时应当报错")
	}
}

func TestIdleWatchdogCancelsWhenIdle(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := newIdleWatchdog(150*time.Millisecond, cancel)
	defer w.Stop()

	select {
	case <-ctx.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("长时间无数据流动时应当取消请求")
	}
}

func TestIdleWatchdogKeepsAliveWhileActive(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	w := newIdleWatchdog(600*time.Millisecond, cancel)
	defer w.Stop()

	// 持续活动约 1.2 秒，远超超时时间，但只要数据在流动就不应被取消
	for i := 0; i < 24; i++ {
		time.Sleep(50 * time.Millisecond)
		w.Touch()
	}
	if ctx.Err() != nil {
		t.Fatal("持续有数据流动时不应取消，否则流式长回答会被误杀")
	}
}
