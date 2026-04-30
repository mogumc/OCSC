package main

import (
	"log"
	"net/http"
	"ocs-ai-proxy/internal/config"
	"ocs-ai-proxy/internal/handler"
	"ocs-ai-proxy/internal/proxy"
)

func main() {
	// 加载配置
	cfg := config.LoadConfig()
	if err := cfg.Validate(); err != nil {
		log.Fatalf("配置校验失败: %v", err)
	}

	// 创建代理实例
	p := proxy.NewLLMProxy(cfg)

	// 注册路由
	chatHandler := handler.NewChatHandler(p)
	mux := http.NewServeMux()
	mux.Handle("/v1/chat/completions", chatHandler)

	// 健康检查
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"status":"ok"}`))
	})

	// 启动服务
	addr := cfg.ListenAddr
	log.Printf("[INFO] OCS AI Proxy 启动，监听地址: %s", addr)
	log.Printf("[INFO] 本地大模型端点: %s", cfg.LLMEndpoint)
	log.Printf("[INFO] 使用模型: %s", cfg.LLMModel)
	log.Printf("[INFO] OCS 网课助手请配置 API 地址为: http://localhost%s/v1", addr)

	if err := http.ListenAndServe(addr, mux); err != nil {
		log.Fatalf("服务启动失败: %v", err)
	}
}
