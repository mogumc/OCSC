package config

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// defaultEnvFile 默认读取的环境变量文件。
const defaultEnvFile = ".env"

// Config 应用运行配置。
//
// 加载优先级：真实环境变量 > .env 文件 > 内置默认值。
// 也就是说 .env 只是「补默认值」，不会覆盖已经存在的环境变量，
// 这样在 Docker / CI 里用 -e 传参依然能压过镜像内的 .env。
type Config struct {
	ListenAddr   string // HTTP 监听地址
	APIKey       string // 上游大模型 API Key，本地 Ollama 可留空
	BaseURL      string // 上游大模型基础地址（OpenAI 兼容，需带 /v1）
	Model        string // 使用的模型名
	TimeoutSec   int    // 单次上游请求超时（秒）
	MaxRetries   int    // 输出解析失败后的最大重试次数
	EnableVision bool   // 是否接受下游传入的图片进行作答
	Stream       bool   // 是否使用上游模型的流式输出（SSE）
	AccessToken  string // 下游访问令牌，留空表示不鉴权
	AuthHeader   string // 携带访问令牌的请求头名

	CacheEnabled bool    // 是否启用答案缓存
	CacheDB      string  // 缓存库文件路径
	CacheRefresh float64 // 命中缓存后重新作答的概率（0~1）

	// 以下两个字段只用于启动日志，不参与业务逻辑。
	EnvFile  string // 实际成功加载的 .env 路径，未加载时为空
	EnvCount int    // 从 .env 载入的变量条数
}

// LoadConfig 加载配置，并尝试从 .env 补齐缺失项。
func LoadConfig() *Config {
	cfg := &Config{}

	if path := firstEnv(defaultEnvFile, "ENV_FILE"); path != "" {
		if n, err := loadDotEnv(path); err == nil {
			cfg.EnvFile, cfg.EnvCount = path, n
		}
	}

	cfg.ListenAddr = firstEnv(":8080", "LISTEN_ADDR")
	cfg.APIKey = firstEnv("", "API_KEY", "OPENAI_API_KEY")
	cfg.BaseURL = firstEnv("http://localhost:11434/v1", "BASE_URL", "LLM_ENDPOINT", "OPENAI_BASE_URL")
	cfg.Model = firstEnv("qwen2.5:7b", "MODEL", "LLM_MODEL", "OPENAI_MODEL")
	cfg.TimeoutSec = firstEnvInt(30, "TIMEOUT_SEC")
	cfg.MaxRetries = firstEnvInt(2, "MAX_RETRIES")
	cfg.EnableVision = firstEnvBool(false, "ENABLE_VISION", "VISION_ENABLED")
	// 流式默认开启：只影响与上游模型的通信方式，下游依旧是完整 JSON，
	// 但能让推理模型的长回答不再被整体超时掐断。
	cfg.Stream = firstEnvBool(true, "STREAM", "ENABLE_STREAM")
	cfg.AccessToken = firstEnv("", "ACCESS_TOKEN", "AUTH_TOKEN", "SERVER_TOKEN")
	cfg.AuthHeader = firstEnv("Authorization", "AUTH_HEADER")
	cfg.CacheEnabled = firstEnvBool(true, "CACHE_ENABLED", "ENABLE_CACHE")
	cfg.CacheDB = firstEnv("answer_cache.db", "CACHE_DB", "CACHE_PATH")
	cfg.CacheRefresh = firstEnvFloat(0.1, "CACHE_RETRY_PROBABILITY", "CACHE_REFRESH_PROBABILITY")

	// 统一去掉结尾斜杠，避免拼出 //chat/completions
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")

	return cfg
}

// Validate 校验配置合法性。
func (c *Config) Validate() error {
	switch {
	case c.ListenAddr == "":
		return fmt.Errorf("LISTEN_ADDR 不能为空")
	case c.BaseURL == "":
		return fmt.Errorf("BASE_URL 不能为空")
	case !strings.HasPrefix(c.BaseURL, "http://") && !strings.HasPrefix(c.BaseURL, "https://"):
		return fmt.Errorf("BASE_URL 必须以 http:// 或 https:// 开头，当前为 %q", c.BaseURL)
	case c.Model == "":
		return fmt.Errorf("MODEL 不能为空")
	case c.TimeoutSec <= 0:
		return fmt.Errorf("TIMEOUT_SEC 必须大于 0，当前为 %d", c.TimeoutSec)
	case c.MaxRetries < 0:
		return fmt.Errorf("MAX_RETRIES 不能为负数，当前为 %d", c.MaxRetries)
	case c.AuthHeader == "":
		return fmt.Errorf("AUTH_HEADER 不能为空")
	case c.CacheEnabled && c.CacheDB == "":
		return fmt.Errorf("启用缓存时 CACHE_DB 不能为空")
	case c.CacheRefresh < 0 || c.CacheRefresh > 1:
		return fmt.Errorf("CACHE_RETRY_PROBABILITY 必须在 0~1 之间，当前为 %v", c.CacheRefresh)
	}
	return nil
}

// MaskedAPIKey 返回脱敏后的上游 API Key，供启动日志展示。
func (c *Config) MaskedAPIKey() string { return mask(c.APIKey) }

// MaskedAccessToken 返回脱敏后的下游访问令牌，供启动日志展示。
func (c *Config) MaskedAccessToken() string { return mask(c.AccessToken) }

// AuthEnabled 表示是否开启了访问令牌校验。
func (c *Config) AuthEnabled() bool { return c.AccessToken != "" }

// mask 只保留首尾各 4 个字符，避免令牌泄漏到日志。
func mask(s string) string {
	switch {
	case s == "":
		return "(未配置)"
	case len(s) <= 8:
		return "****"
	default:
		return s[:4] + "****" + s[len(s)-4:]
	}
}

// loadDotEnv 逐行解析 .env 并写入进程环境变量，返回写入的条目数。
// 文件不存在不算错误，交由 callers 按「未加载」处理。
func loadDotEnv(path string) (int, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	count := 0
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, val, ok := parseEnvLine(scanner.Text())
		if !ok {
			continue
		}
		if _, exists := os.LookupEnv(key); exists {
			continue // 真实环境变量优先，不覆盖
		}
		if err := os.Setenv(key, val); err != nil {
			continue
		}
		count++
	}
	return count, scanner.Err()
}

// parseEnvLine 解析单行 .env 内容，支持 # 注释、export 前缀、引号包裹的值。
func parseEnvLine(line string) (key, val string, ok bool) {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, "#") {
		return "", "", false
	}

	line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	key, val, found := strings.Cut(line, "=")
	if !found {
		return "", "", false
	}

	key = strings.TrimSpace(key)
	if key == "" {
		return "", "", false
	}
	return key, parseEnvValue(strings.TrimSpace(val)), true
}

// parseEnvValue 解析值的部分：剥掉引号、去掉行尾注释。
//
// 顺序很关键：先判断引号，再考虑注释。
// 否则 "pass # word" 这种值里的 # 会被误当成注释起点。
func parseEnvValue(val string) string {
	if val != "" && (val[0] == '"' || val[0] == '\'') {
		if end := strings.IndexByte(val[1:], val[0]); end != -1 {
			return val[1 : 1+end]
		}
		return val[1:] // 引号没闭合，退化为去掉首字符
	}
	if idx := strings.Index(val, " #"); idx != -1 {
		val = val[:idx]
	}
	return strings.TrimSpace(val)
}

// firstEnv 返回第一个非空的环境变量值，全部为空时返回默认值。
// 支持多个候选名，用于兼容历史变量名。
func firstEnv(defaultVal string, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(os.Getenv(key)); v != "" {
			return v
		}
	}
	return defaultVal
}

func firstEnvInt(defaultVal int, keys ...string) int {
	for _, key := range keys {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			continue
		}
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}

func firstEnvBool(defaultVal bool, keys ...string) bool {
	for _, key := range keys {
		switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
		case "":
			continue
		case "1", "true", "yes", "y", "on":
			return true
		case "0", "false", "no", "n", "off":
			return false
		}
	}
	return defaultVal
}

func firstEnvFloat(defaultVal float64, keys ...string) float64 {
	for _, key := range keys {
		v := strings.TrimSpace(os.Getenv(key))
		if v == "" {
			continue
		}
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	}
	return defaultVal
}
