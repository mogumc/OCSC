package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"ocs-ai-proxy/internal/config"
	"ocs-ai-proxy/internal/logx"
	"ocs-ai-proxy/internal/model"
)

const (
	// maxImages 单次请求允许携带的图片上限，避免请求体过大拖垮上游。
	maxImages = 8
	// maxRespBytes 读取上游响应的上限（4 MiB）。
	maxRespBytes = 4 << 20
	// maxErrBytes 读取上游错误响应的上限，够看报错就行。
	maxErrBytes = 8 << 10
	// defaultImageMime 裸 base64 图片未声明类型时的兜底 MIME。
	defaultImageMime = "image/png"

	// sseDataPrefix SSE 数据行前缀。
	sseDataPrefix = "data:"
	// sseDone SSE 流结束标记。
	sseDone = "[DONE]"

	// thinkOpenTag / thinkCloseTag 是思维链标签。
	// 这是本地/自建推理模型（Qwen3、R1 蒸馏版、Ollama 等）把思维链内联在
	// content 里时的写法，属于需要清洗的「入站」内容，不是我们要求的输出格式。
	// 云端官方 API 已改为把推理放在独立的 reasoning_content 字段，见 streamChunk。
	thinkOpenTag  = "<think>"
	thinkCloseTag = "</think>"
)

var (
	// thinkBlockRe 匹配成对闭合的思维链。
	thinkBlockRe = regexp.MustCompile(`(?s)` + regexp.QuoteMeta(thinkOpenTag) + `.*?` + regexp.QuoteMeta(thinkCloseTag))
	jsonBlockRe  = regexp.MustCompile(`(?s)\{.*\}`)
)

// streamChunk 是流式响应中的单个增量分片。
//
// delta.content 才是答案正文；reasoning_content 是推理模型单独返回的思维链
// （DeepSeek-R1 等官方 API 的做法），一律不参与答案拼接。
// 个别服务商把该字段命名为 reasoning，这里一并兼容。
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
			Reasoning        string `json:"reasoning"`
		} `json:"delta"`
	} `json:"choices"`
}

// sseStream 是一次流式读取的结果。
type sseStream struct {
	// Content 拼接后的答案正文。
	Content string
	// HasReasoning 上游是否单独返回过推理内容。
	HasReasoning bool
	// IsSSE 上游是否真的按 SSE 返回；false 时 Content 实际是原始响应文本。
	IsSSE bool
}

// LLMProxy 把 OCS 题库请求转换为 OpenAI 兼容（必要时多模态）请求，并解析出结构化答案。
//
// 下游始终是一次性的 OCS JSON 响应；「流式」只发生在与上游模型之间，
// 用于让长回答（尤其是推理模型）不被整体超时掐断。
type LLMProxy struct {
	client *http.Client
	cfg    *config.Config
}

// NewLLMProxy 创建代理实例。
func NewLLMProxy(cfg *config.Config) *LLMProxy {
	idle := time.Duration(cfg.TimeoutSec) * time.Second

	// 建连与响应头单独限时，响应体阶段交给空闲看门狗，
	// 这样「持续输出但不结束」不会被误判成超时。
	dialTimeout := 10 * time.Second
	if idle < dialTimeout {
		dialTimeout = idle
	}

	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		DialContext:           (&net.Dialer{Timeout: dialTimeout, KeepAlive: 30 * time.Second}).DialContext,
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   dialTimeout,
		ResponseHeaderTimeout: idle,
		IdleConnTimeout:       90 * time.Second,
		ExpectContinueTimeout: time.Second,
		MaxIdleConns:          32,
		MaxIdleConnsPerHost:   16,
	}

	return &LLMProxy{
		cfg: cfg,
		// 这里刻意不设 http.Client.Timeout：它是「整次请求」的总时长上限，
		// 会把流式长回答直接掐断。超时统一由空闲看门狗负责。
		client: &http.Client{Transport: transport},
	}
}

// Answer 向上游模型提问，返回解析后的结构化答案。
// 调用失败或输出无法解析时会按 MAX_RETRIES 重试。
func (p *LLMProxy) Answer(req *model.SearchRequest) (*model.AnswerResult, error) {
	images := p.resolveImages(req.Images)

	var lastErr error
	for attempt := 1; attempt <= p.cfg.MaxRetries+1; attempt++ {
		result, err := p.askOnce(req, images)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt <= p.cfg.MaxRetries {
			logx.Warn("第 %d 次作答失败，准备重试: %v", attempt, err)
		}
	}
	return nil, fmt.Errorf("重试 %d 次后仍失败: %w", p.cfg.MaxRetries, lastErr)
}

// resolveImages 依据 ENABLE_VISION 决定本次实际携带的图片。
// 开关关闭时忽略下游图片并告警，保证 OCS 默认配置（不带图）行为不变。
func (p *LLMProxy) resolveImages(raw []string) []string {
	if len(raw) == 0 {
		return nil
	}
	if !p.cfg.EnableVision {
		logx.Warn("收到 %d 张图片，但 ENABLE_VISION 未开启，已忽略", len(raw))
		return nil
	}

	images := make([]string, 0, len(raw))
	for i, img := range raw {
		if len(images) >= maxImages {
			logx.Warn("图片数量超过上限 %d，其余 %d 张已忽略", maxImages, len(raw)-i)
			break
		}
		if ref, ok := normalizeImageRef(img); ok {
			images = append(images, ref)
		}
	}
	return images
}

// normalizeImageRef 把下游传入的图片字段规整为模型可识别的引用。
// 支持 data URI、http(s) 链接，以及裸 base64（按 PNG 处理）。
func normalizeImageRef(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	switch {
	case raw == "":
		return "", false
	case strings.HasPrefix(raw, "data:image/"):
		return raw, true
	case strings.HasPrefix(raw, "http://"), strings.HasPrefix(raw, "https://"):
		return raw, true
	default:
		return "data:" + defaultImageMime + ";base64," + raw, true
	}
}

// askOnce 执行一次完整的调用与解析。
func (p *LLMProxy) askOnce(req *model.SearchRequest, images []string) (*model.AnswerResult, error) {
	payload, err := json.Marshal(model.CompletionRequest{
		Model:       p.cfg.Model,
		Messages:    buildMessages(req, images),
		Temperature: 0.3,
		Stream:      p.cfg.Stream,
	})
	if err != nil {
		return nil, fmt.Errorf("序列化请求失败: %w", err)
	}

	// 请求级 context 用于在空闲超时时中断流式读取。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, p.cfg.BaseURL+"/chat/completions", bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("构建上游请求失败: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if p.cfg.Stream {
		httpReq.Header.Set("Accept", "text/event-stream")
	}
	if p.cfg.APIKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+p.cfg.APIKey)
	}

	resp, err := p.client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("请求上游模型失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrBytes))
		return nil, fmt.Errorf("上游返回状态码 %d: %s", resp.StatusCode, truncate(string(body), 300))
	}

	watchdog := newIdleWatchdog(time.Duration(p.cfg.TimeoutSec)*time.Second, cancel)
	defer watchdog.Stop()

	content, err := p.readAnswerContent(resp.Body, watchdog.Touch)
	if err != nil {
		return nil, err
	}
	return parseAnswer(content)
}

// readAnswerContent 从上游响应里取出模型的文本输出。
//
// 流式模式下拼接 SSE 增量；非流式模式下解析整体 JSON。
// 若流式请求被上游忽略（返回的是普通 JSON），会自动退化为整体解析，
// 避免因为上游不支持 stream 而整条链路不可用。
func (p *LLMProxy) readAnswerContent(body io.Reader, onActivity func()) (string, error) {
	limited := io.LimitReader(body, maxRespBytes)

	if !p.cfg.Stream {
		raw, err := io.ReadAll(limited)
		if err != nil {
			return "", fmt.Errorf("读取上游响应失败: %w", err)
		}
		return contentFromCompletion(raw)
	}

	stream, err := readSSE(limited, onActivity)
	if err != nil {
		return "", err
	}
	if !stream.IsSSE {
		logx.Warn("上游未按流式返回，已退化为整体 JSON 解析")
		return contentFromCompletion([]byte(stream.Content))
	}
	if strings.TrimSpace(stream.Content) == "" && stream.HasReasoning {
		return "", errReasoningOnly
	}
	return stream.Content, nil
}

// errReasoningOnly 表示模型只产出了推理内容、没有给出最终答案。
// 常见于推理模型被 max_tokens 截断，或上游把两者放在同一个字段里。
var errReasoningOnly = errors.New(
	"模型只返回了推理内容（reasoning_content），未产出最终答案，通常是被 max_tokens 截断")

// contentFromCompletion 从整体 JSON 响应中取出 content 字段。
//
// 推理模型会把思维链放在 reasoning_content 里，这里不读该字段；
// 但如果 content 是空的而推理内容非空，说明答案被截断了，给出可定位的报错。
func contentFromCompletion(body []byte) (string, error) {
	var completion model.CompletionResponse
	if err := json.Unmarshal(body, &completion); err != nil {
		return "", fmt.Errorf("解析上游响应失败: %w", err)
	}
	if len(completion.Choices) == 0 {
		return "", errors.New("上游响应中没有 choices")
	}

	message := completion.Choices[0].Message
	if strings.TrimSpace(message.Content) == "" && strings.TrimSpace(message.ReasoningContent) != "" {
		return "", errReasoningOnly
	}
	return message.Content, nil
}

// readSSE 消费上游的 SSE 流并拼接增量内容。
//
// 返回结果里 IsSSE=false 时，Content 是原始响应文本（上游没按流式返回，
// 交由调用方整体解析）。onActivity 在每读到一段数据时回调，用于给空闲看门狗续期。
func readSSE(body io.Reader, onActivity func()) (sseStream, error) {
	reader := bufio.NewReader(body)

	var result sseStream
	var content, raw strings.Builder

	for {
		line, readErr := reader.ReadString('\n')
		if line != "" {
			if onActivity != nil {
				onActivity()
			}
		}

		payload, isDataLine := ssePayload(line)
		switch {
		case isDataLine:
			result.IsSSE = true
			if payload == sseDone {
				result.Content = content.String()
				return result, nil
			}
			var chunk streamChunk
			if json.Unmarshal([]byte(payload), &chunk) == nil {
				for _, choice := range chunk.Choices {
					// 只拼 content，推理字段明确丢弃
					content.WriteString(choice.Delta.Content)
					if choice.Delta.ReasoningContent != "" || choice.Delta.Reasoning != "" {
						result.HasReasoning = true
					}
				}
			}
		case !result.IsSSE:
			// 还没出现 data: 行，先留一份原文以备整体解析
			raw.WriteString(line)
		}

		if readErr != nil {
			if errors.Is(readErr, io.EOF) {
				break
			}
			return sseStream{}, fmt.Errorf("读取上游流式响应失败: %w", readErr)
		}
	}

	if !result.IsSSE {
		result.Content = raw.String()
		return result, nil
	}
	result.Content = content.String()
	return result, nil
}

// ssePayload 取出一行 SSE 的数据部分，非 data 行返回 ok=false。
func ssePayload(line string) (string, bool) {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, sseDataPrefix) {
		return "", false
	}
	return strings.TrimSpace(strings.TrimPrefix(line, sseDataPrefix)), true
}

// parseAnswer 清洗模型输出并解析为 AnswerResult。
func parseAnswer(content string) (*model.AnswerResult, error) {
	cleaned := cleanContent(content)
	if cleaned == "" {
		return nil, errors.New("清洗后模型输出为空，通常是被截断，或只输出了思维链")
	}

	var result model.AnswerResult
	if err := json.Unmarshal([]byte(cleaned), &result); err != nil {
		return nil, fmt.Errorf("解析答案 JSON 失败: %w，原文: %s", err, truncate(cleaned, 200))
	}

	result.Answer = strings.TrimSpace(result.Answer)
	result.Analysis = strings.TrimSpace(result.Analysis)
	if result.Answer == "" {
		return nil, fmt.Errorf("answer 字段为空，原文: %s", truncate(cleaned, 200))
	}
	return &result, nil
}

// cleanContent 清洗模型输出，取出其中的答案 JSON。
//
// 处理三类情况：
//  1. 成对闭合的思维链（<think>…</think>）整段去掉；
//  2. **思维链没有闭合**（模型被截断）时，从 <think> 开始的内容全部丢弃——
//     否则推理过程里出现的 JSON 会被误当成答案；
//  3. 剥掉 Markdown 代码块围栏，并截取最外层的 JSON 主体。
func cleanContent(content string) string {
	s := thinkBlockRe.ReplaceAllString(content, "")

	// 处理未闭合的思维链：宁可丢弃后半段，也不能让推理里的 JSON 冒充答案
	if idx := strings.Index(s, thinkOpenTag); idx != -1 {
		s = s[:idx]
	}

	s = strings.TrimSpace(s)
	s = strings.TrimPrefix(s, "```json")
	s = strings.TrimPrefix(s, "```JSON")
	s = strings.TrimPrefix(s, "```")
	s = strings.TrimSuffix(s, "```")
	s = strings.TrimSpace(s)

	if match := jsonBlockRe.FindString(s); match != "" {
		return match
	}
	return s
}

// truncate 按字符（而非字节）截断，避免切出非法 UTF-8。
func truncate(s string, maxLen int) string {
	runes := []rune(s)
	if len(runes) <= maxLen {
		return s
	}
	return string(runes[:maxLen]) + "..."
}

// idleWatchdog 在「持续一段时间没有任何数据流动」时取消请求。
//
// 流式回答可能连续输出几十秒甚至更久，用整体超时会误杀；
// 因此这里做空闲检测：只要还有数据在流动就不算超时。
type idleWatchdog struct {
	timeout time.Duration
	last    atomic.Int64
	cancel  context.CancelFunc
	done    chan struct{}
	once    sync.Once
}

// newIdleWatchdog 创建并启动看门狗，超时时间为 timeout。
func newIdleWatchdog(timeout time.Duration, cancel context.CancelFunc) *idleWatchdog {
	w := &idleWatchdog{
		timeout: timeout,
		cancel:  cancel,
		done:    make(chan struct{}),
	}
	w.Touch()

	// 以 1/4 周期轮询，兼顾及时性与开销
	interval := timeout / 4
	if interval < 100*time.Millisecond {
		interval = 100 * time.Millisecond
	}
	go w.watch(interval)

	return w
}

// Touch 记录一次活动，重置空闲计时。
func (w *idleWatchdog) Touch() {
	w.last.Store(time.Now().UnixNano())
}

// Stop 停止看门狗。
func (w *idleWatchdog) Stop() {
	w.once.Do(func() { close(w.done) })
}

func (w *idleWatchdog) watch(interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			if time.Since(time.Unix(0, w.last.Load())) >= w.timeout {
				w.cancel()
				return
			}
		case <-w.done:
			return
		}
	}
}
