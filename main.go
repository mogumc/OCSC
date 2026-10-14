package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"ocs-ai-proxy/internal/cache"
	"ocs-ai-proxy/internal/config"
	"ocs-ai-proxy/internal/handler"
	"ocs-ai-proxy/internal/logx"
	"ocs-ai-proxy/internal/proxy"
)

// version 由构建时注入：-ldflags "-X main.version=vX.Y.Z"。
var version = "dev"

func main() {
	healthcheck := flag.Bool("healthcheck", false, "以容器健康检查模式运行：探测本机 /health 后退出")
	flag.Parse()

	cfg := config.LoadConfig()

	if *healthcheck {
		os.Exit(runHealthcheck(cfg))
	}

	if err := cfg.Validate(); err != nil {
		logx.Fatal("配置校验失败: %v", err)
	}

	answerCache, err := openCache(cfg)
	if err != nil {
		logx.Fatal("初始化答案缓存失败: %v", err)
	}
	defer answerCache.Close()

	mux := http.NewServeMux()
	searchHandler := handler.NewSearchHandler(
		proxy.NewLLMProxy(cfg), answerCache, cfg.CacheRefresh,
	)
	// 只保护会产生模型费用的 /search；/ 与 /health 保持开放，方便探活。
	mux.Handle("/search", handler.AuthGuard(cfg.AccessToken, cfg.AuthHeader, searchHandler))
	mux.HandleFunc("/health", handleHealth)
	mux.HandleFunc("/", handleIndex)

	printBanner(cfg, answerCache)

	server := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           mux,
		ReadHeaderTimeout: 10 * time.Second,
	}

	// 优雅退出：容器收到 SIGTERM 时先停止接收新请求。
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go func() {
		<-ctx.Done()
		logx.Info("收到退出信号，正在关闭服务...")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	}()

	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		logx.Fatal("服务启动失败: %v", err)
	}
	logx.Info("服务已退出")
}

// handleHealth 供容器健康检查与探活使用。
func handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleIndex 处理 GET/HEAD /，其余路径统一返回 404。
func handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_, _ = w.Write([]byte(`{"code":1,"msg":"OCS Copilot is running"}`))
}

// runHealthcheck 探测本机健康检查接口并返回进程退出码，
// 这样 distroless 镜像无需 shell / curl 也能做容器健康检查。
func runHealthcheck(cfg *config.Config) int {
	url := "http://127.0.0.1:" + portOf(cfg.ListenAddr) + "/health"

	resp, err := (&http.Client{Timeout: 3 * time.Second}).Get(url)
	if err != nil {
		fmt.Fprintf(os.Stderr, "healthcheck 失败: %v\n", err)
		return 1
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Fprintf(os.Stderr, "healthcheck 返回状态码 %d\n", resp.StatusCode)
		return 1
	}
	return 0
}

// portOf 从监听地址中取出端口号，如 ":8080" -> "8080"。
func portOf(addr string) string {
	if i := strings.LastIndex(addr, ":"); i >= 0 {
		return addr[i+1:]
	}
	return addr
}

// displayAddr 把监听地址补全成可直接粘贴的地址，
// ":8080" -> "localhost:8080"，"127.0.0.1:8080" 原样返回。
func displayAddr(addr string) string {
	if strings.HasPrefix(addr, ":") {
		return "localhost" + addr
	}
	return addr
}

// openCache 按配置打开答案缓存；未启用时返回 nil（调用方需容忍 nil）。
func openCache(cfg *config.Config) (*cache.Cache, error) {
	if !cfg.CacheEnabled {
		return nil, nil
	}
	return cache.Open(cfg.CacheDB)
}

// printBanner 打印启动信息。API Key 与访问令牌只输出脱敏结果。
func printBanner(cfg *config.Config, answerCache *cache.Cache) {
	logx.Success("OCS Copilot %s 已启动，监听地址: %s", version, cfg.ListenAddr)
	if cfg.EnvFile != "" {
		logx.Info("已从 %s 载入 %d 项配置", cfg.EnvFile, cfg.EnvCount)
	}
	logx.Info("上游模型端点: %s", cfg.BaseURL)
	logx.Info("使用模型: %s", cfg.Model)
	logx.Info("上游 API Key: %s", cfg.MaskedAPIKey())
	logx.Info("图片作答: %s", onOff(cfg.EnableVision))
	logx.Info("上游流式输出: %s", onOff(cfg.Stream))
	logx.Info("上游空闲超时 %d 秒 / 最大重试 %d 次", cfg.TimeoutSec, cfg.MaxRetries)

	switch {
	case answerCache == nil:
		logx.Info("答案缓存: 已关闭")
	default:
		n, err := answerCache.Count()
		if err != nil {
			logx.Info("答案缓存: 已开启（%s）", cfg.CacheDB)
		} else {
			logx.Info("答案缓存: 已开启（%s，已有 %d 条，刷新概率 %.0f%%）",
				cfg.CacheDB, n, cfg.CacheRefresh*100)
		}
	}

	if cfg.AuthEnabled() {
		logx.Info("访问令牌: %s（请求头 %s）", cfg.MaskedAccessToken(), cfg.AuthHeader)
	} else {
		logx.Warn("未设置 ACCESS_TOKEN，/search 任何人可调用；私人部署请务必配置")
	}
	logx.Info("OCS 题库地址请填: http://%s/search", displayAddr(cfg.ListenAddr))
}

func onOff(b bool) string {
	if b {
		return "已开启"
	}
	return "已关闭"
}
