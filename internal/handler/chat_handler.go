package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"ocs-ai-proxy/internal/model"
	"ocs-ai-proxy/internal/proxy"
)

// ChatHandler HTTP 处理器
type ChatHandler struct {
	proxy *proxy.LLMProxy
}

// NewChatHandler 创建处理器实例
func NewChatHandler(p *proxy.LLMProxy) *ChatHandler {
	return &ChatHandler{proxy: p}
}

// ServeHTTP 处理 /v1/chat/completions 请求
func (h *ChatHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "仅支持 POST 方法")
		return
	}

	var req model.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "请求体 JSON 解析失败: "+err.Error())
		return
	}

	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "messages 字段不能为空")
		return
	}

	resp, answer, err := h.proxy.ChatWithRetry(&req)
	if err != nil {
		log.Printf("[WARN] 模型调用失败: %v", err)
		fallbackResp := proxy.BuildFallbackResponse(err.Error())
		writeJSON(w, http.StatusOK, fallbackResp)
		return
	}

	log.Printf("[INFO] 答案: %s (置信度: %.2f)", answer.Answer, answer.Confidence)

	writeJSON(w, http.StatusOK, resp)
}

// writeJSON 写入 JSON 响应
func writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		log.Printf("[ERROR] 响应序列化失败: %v", err)
	}
}

// writeError 写入错误响应
func writeError(w http.ResponseWriter, status int, msg string) {
	log.Printf("[WARN] %d %s", status, msg)
	writeJSON(w, status, map[string]string{"error": msg})
}
