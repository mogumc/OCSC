package config

import (
	"fmt"
	"os"
	"strconv"
)

// Config 应用配置，从环境变量加载
type Config struct {
	ListenAddr  string // HTTP 监听地址
	LLMEndpoint string // 本地大模型 API 地址
	LLMModel    string // 使用的模型名称
	TimeoutSec  int    // 请求超时秒数
	MaxRetries  int    // 解析失败重试次数
}

// LoadConfig 从环境变量读取配置，缺失时使用默认值
func LoadConfig() *Config {
	return &Config{
		ListenAddr:  getEnv("LISTEN_ADDR", ":8080"),
		LLMEndpoint: getEnv("LLM_ENDPOINT", "http://localhost:11434/v1"),
		LLMModel:    getEnv("LLM_MODEL", "qwen2.5:7b"),
		TimeoutSec:  getEnvInt("TIMEOUT_SEC", 30),
		MaxRetries:  getEnvInt("MAX_RETRIES", 2),
	}
}

// Validate 校验配置合法性
func (c *Config) Validate() error {
	if c.ListenAddr == "" {
		return fmt.Errorf("LISTEN_ADDR 不能为空")
	}
	if c.LLMEndpoint == "" {
		return fmt.Errorf("LLM_ENDPOINT 不能为空")
	}
	if c.LLMModel == "" {
		return fmt.Errorf("LLM_MODEL 不能为空")
	}
	if c.TimeoutSec <= 0 {
		return fmt.Errorf("TIMEOUT_SEC 必须大于 0")
	}
	if c.MaxRetries < 0 {
		return fmt.Errorf("MAX_RETRIES 不能为负数")
	}
	return nil
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func getEnvInt(key string, defaultVal int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return defaultVal
}
