package handler

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"ocs-ai-proxy/internal/cache"
	"ocs-ai-proxy/internal/logx"
	"ocs-ai-proxy/internal/model"
	"ocs-ai-proxy/internal/proxy"
)

// maxBodyBytes 限制请求体大小（32 MiB），因为带图请求可能是 base64 大包。
const maxBodyBytes = 32 << 20

// SearchHandler 处理 OCS 题库协议请求。
type SearchHandler struct {
	proxy *proxy.LLMProxy

	// cache 为 nil 表示未启用缓存。
	cache *cache.Cache

	// refreshProbability 是命中缓存后重新作答的概率，用来避免错误答案被永久缓存。
	refreshProbability float64
}

// NewSearchHandler 创建处理器实例。answerCache 传 nil 即关闭缓存能力。
func NewSearchHandler(p *proxy.LLMProxy, answerCache *cache.Cache, refreshProbability float64) *SearchHandler {
	return &SearchHandler{
		proxy:              p,
		cache:              answerCache,
		refreshProbability: refreshProbability,
	}
}

// ServeHTTP 处理 POST /search。
func (h *SearchHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	// 每次请求都记一条耗时，供后台观测线上真实模型的响应速度。
	// 精度取到 0.1 秒即可：走真实模型是秒级，命中缓存时接近 0。
	defer func() {
		logx.Field("耗时", fmt.Sprintf("%.1fs", time.Since(start).Seconds()))
	}()

	if r.Method != http.MethodPost {
		writeResponse(w, http.StatusMethodNotAllowed, model.SearchResponse{Code: 0, Msg: "仅支持 POST 方法"})
		return
	}

	var req model.SearchRequest
	if err := json.NewDecoder(io.LimitReader(r.Body, maxBodyBytes)).Decode(&req); err != nil {
		writeResponse(w, http.StatusBadRequest, model.SearchResponse{Code: 0, Msg: "请求体 JSON 解析失败: " + err.Error()})
		return
	}

	req.Title = strings.TrimSpace(req.Title)
	if req.Title == "" {
		writeResponse(w, http.StatusBadRequest, model.SearchResponse{Code: 0, Msg: "题目为空"})
		return
	}
	// 先规整选项再算缓存键，这样排版差异不会让同一道题算出两个键
	req.Options = cleanOptions(req.Options)

	logx.Request(req.Title, model.TypeLabel(req.Type), req.Options, len(req.Images))

	key := h.cacheKey(&req)
	if h.answerFromCache(w, key, &req) {
		return
	}

	result, err := h.proxy.Answer(&req)
	if err != nil {
		logx.Error("作答失败: %v", err)
		// 仍返回 HTTP 200 + code=0，OCS 的 handler 才能读到 msg 并继续尝试后续题库。
		writeResponse(w, http.StatusOK, model.SearchResponse{Code: 0, Msg: "模型调用失败: " + err.Error()})
		return
	}

	logx.Response(result.Answer, result.Analysis)
	h.storeAnswer(key, &req, result)

	writeResponse(w, http.StatusOK, model.SearchResponse{
		Code:     1,
		Question: req.Title,
		Answer:   result.Answer,
		Analysis: result.Analysis,
	})
}

// cacheKey 返回缓存键；未启用缓存时返回空串。
func (h *SearchHandler) cacheKey(req *model.SearchRequest) string {
	if h.cache == nil {
		return ""
	}
	return cache.Key(req)
}

// answerFromCache 尝试直接用缓存作答，已把响应写给调用方时返回 true。
//
// 缓存读取出错一律按未命中处理：缓存只是加速手段，不能因为它故障导致答题失败。
func (h *SearchHandler) answerFromCache(w http.ResponseWriter, key string, req *model.SearchRequest) bool {
	if key == "" || req.SkipCache {
		return false
	}

	entry, err := h.cache.Get(key)
	if err != nil {
		logx.Warn("读取缓存失败，按未命中处理: %v", err)
		return false
	}
	if entry == nil {
		logx.Field("缓存", "未命中")
		return false
	}

	// 按概率挑一部分请求重新作答，避免错误答案被一直缓存住
	if cache.ShouldRefresh(h.refreshProbability) {
		logx.Field("缓存", "命中，本次重新作答并刷新")
		return false
	}

	logx.Field("缓存", "命中")
	logx.Response(entry.Answer, entry.Analysis)

	writeResponse(w, http.StatusOK, model.SearchResponse{
		Code:     1,
		Question: req.Title,
		Answer:   entry.Answer,
		Analysis: entry.Analysis,
	})
	return true
}

// storeAnswer 把新答案写入缓存。写失败只告警，不影响本次作答结果。
func (h *SearchHandler) storeAnswer(key string, req *model.SearchRequest, result *model.AnswerResult) {
	if key == "" {
		return
	}
	if err := h.cache.Put(key, req, &cache.Entry{Answer: result.Answer, Analysis: result.Analysis}); err != nil {
		logx.Warn("写入缓存失败: %v", err)
	}
}

// cleanOptions 清理选项文本：逐行去空白并丢弃空行，提高模型识别准确率。
func cleanOptions(options string) string {
	lines := strings.Split(options, "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			kept = append(kept, line)
		}
	}
	return strings.Join(kept, "\n")
}

// writeResponse 写入 OCS 协议响应。
func writeResponse(w http.ResponseWriter, status int, resp model.SearchResponse) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logx.Error("响应序列化失败: %v", err)
	}
}
