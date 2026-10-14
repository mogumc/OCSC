package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseEnvLine(t *testing.T) {
	cases := []struct {
		name  string
		line  string
		key   string
		value string
		ok    bool
	}{
		{"普通键值", "BASE_URL=http://localhost:11434/v1", "BASE_URL", "http://localhost:11434/v1", true},
		{"双引号", `API_KEY="mock-secret-value"`, "API_KEY", "mock-secret-value", true},
		{"单引号", `MODEL='demo-model'`, "MODEL", "demo-model", true},
		{"双引号加行尾注释", `BASE_URL="http://127.0.0.1:8080/v1"   # 端点`, "BASE_URL", "http://127.0.0.1:8080/v1", true},
		{"单引号加行尾注释", `MODEL='demo-model' # 模型`, "MODEL", "demo-model", true},
		{"引号内保留井号", `API_KEY="mock-secret # 不是注释"`, "API_KEY", "mock-secret # 不是注释", true},
		{"引号未闭合", `MODEL="demo-model`, "MODEL", "demo-model", true},
		{"export 前缀", "export LISTEN_ADDR=:8080", "LISTEN_ADDR", ":8080", true},
		{"行尾注释", "TIMEOUT_SEC=60 # 秒", "TIMEOUT_SEC", "60", true},
		{"等号在值里", "BASE_URL=http://host/v1?a=1", "BASE_URL", "http://host/v1?a=1", true},
		{"空行", "   ", "", "", false},
		{"注释行", "# 这是注释", "", "", false},
		{"缺少等号", "NOT_A_PAIR", "", "", false},
		{"空键名", "=value", "", "", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			key, value, ok := parseEnvLine(c.line)
			if ok != c.ok || key != c.key || value != c.value {
				t.Fatalf("parseEnvLine(%q) = (%q, %q, %v)，期望 (%q, %q, %v)",
					c.line, key, value, ok, c.key, c.value, c.ok)
			}
		})
	}
}

func TestLoadDotEnvDoesNotOverrideExisting(t *testing.T) {
	const (
		overriddenKey = "OCSC_TEST_OVERRIDDEN"
		freshKey      = "OCSC_TEST_FRESH"
	)
	t.Setenv(overriddenKey, "来自真实环境变量")

	path := filepath.Join(t.TempDir(), ".env")
	content := overriddenKey + "=来自文件\n" + freshKey + "=来自文件\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入临时 .env 失败: %v", err)
	}

	count, err := loadDotEnv(path)
	if err != nil {
		t.Fatalf("loadDotEnv 返回错误: %v", err)
	}
	// 只应写入未被占用的那个键
	if count != 1 {
		t.Fatalf("载入条数 = %d，期望 1", count)
	}
	t.Cleanup(func() { os.Unsetenv(freshKey) })

	if got := os.Getenv(overriddenKey); got != "来自真实环境变量" {
		t.Errorf("真实环境变量被 .env 覆盖了: %q", got)
	}
	if got := os.Getenv(freshKey); got != "来自文件" {
		t.Errorf("%s = %q，期望 %q", freshKey, got, "来自文件")
	}
}

func TestLoadDotEnvMissingFile(t *testing.T) {
	if _, err := loadDotEnv(filepath.Join(t.TempDir(), "not-exist.env")); err == nil {
		t.Fatal("文件不存在时应当返回错误，交由调用方按未加载处理")
	}
}

func TestValidate(t *testing.T) {
	valid := Config{
		ListenAddr: ":8080",
		BaseURL:    "http://localhost:11434/v1",
		Model:      "demo-model",
		TimeoutSec: 30,
		MaxRetries: 2,
		AuthHeader: "Authorization",
	}
	if err := valid.Validate(); err != nil {
		t.Fatalf("合法配置不应报错: %v", err)
	}

	cases := map[string]func(*Config){
		"监听地址为空": func(c *Config) { c.ListenAddr = "" },
		"端点协议不对": func(c *Config) { c.BaseURL = "localhost:11434" },
		"模型为空":   func(c *Config) { c.Model = "" },
		"超时非正数":  func(c *Config) { c.TimeoutSec = 0 },
		"重试为负数":  func(c *Config) { c.MaxRetries = -1 },
		"请求头为空":  func(c *Config) { c.AuthHeader = "" },
	}

	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			cfg := valid
			mutate(&cfg)
			if err := cfg.Validate(); err == nil {
				t.Fatalf("期望校验失败，实际通过")
			}
		})
	}
}

func TestFirstEnvBoolDefaults(t *testing.T) {
	const flag = "OCSC_TEST_UNSET_FLAG"
	os.Unsetenv(flag)

	// 流式输出默认开启，靠的就是这个默认值语义
	if !firstEnvBool(true, flag) {
		t.Error("未设置时应当返回默认值 true")
	}
	if firstEnvBool(false, flag) {
		t.Error("未设置时应当返回默认值 false")
	}

	cases := []struct {
		raw  string
		want bool
	}{
		{"true", true}, {"TRUE", true}, {"1", true}, {"yes", true}, {"on", true},
		{"false", false}, {"0", false}, {"no", false}, {"off", false}, {"  no  ", false},
	}
	for _, c := range cases {
		t.Setenv(flag, c.raw)
		if got := firstEnvBool(!c.want, flag); got != c.want {
			t.Errorf("firstEnvBool(%q) = %v，期望 %v", c.raw, got, c.want)
		}
	}
}

func TestFirstEnvAliasPriority(t *testing.T) {
	const (
		primary = "OCSC_TEST_PRIMARY"
		alias   = "OCSC_TEST_ALIAS"
	)
	t.Setenv(primary, "主名称的值")
	t.Setenv(alias, "别名的值")

	if got := firstEnv("默认", primary, alias); got != "主名称的值" {
		t.Errorf("应当优先取第一个候选: %q", got)
	}

	os.Unsetenv(primary)
	if got := firstEnv("默认", primary, alias); got != "别名的值" {
		t.Errorf("主名称为空时应当回退到别名: %q", got)
	}

	os.Unsetenv(alias)
	if got := firstEnv("默认", primary, alias); got != "默认" {
		t.Errorf("都为空时应当返回默认值: %q", got)
	}
}

func TestMaskAndAuthEnabled(t *testing.T) {
	cases := []struct {
		token string
		want  string
	}{
		{"", "(未配置)"},
		{"short", "****"},
		{"mock-token-abcdef123456", "mock****3456"},
	}
	for _, c := range cases {
		cfg := Config{AccessToken: c.token}
		if got := cfg.MaskedAccessToken(); got != c.want {
			t.Errorf("MaskedAccessToken(%q) = %q，期望 %q", c.token, got, c.want)
		}
		if got, want := cfg.AuthEnabled(), c.token != ""; got != want {
			t.Errorf("AuthEnabled(%q) = %v，期望 %v", c.token, got, want)
		}
	}
}
