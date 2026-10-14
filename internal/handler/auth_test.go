package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"ocs-ai-proxy/internal/model"
)

const testToken = "mock-access-token-0123456789"

func newGuardedServer(token, header string) http.Handler {
	ok := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"code":1,"answer":"ok"}`))
	})
	return AuthGuard(token, header, ok)
}

func TestValidToken(t *testing.T) {
	cases := []struct {
		name string
		got  string
		want bool
	}{
		{"裸令牌", testToken, true},
		{"Bearer 前缀", "Bearer " + testToken, true},
		{"小写 bearer", "bearer " + testToken, true},
		{"Bearer 无空格", "Bearer" + testToken, false},
		{"令牌错误", "mock-access-token-wrong", false},
		{"令牌为空", "", false},
		{"仅前缀", "Bearer ", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := validToken(c.got, testToken); got != c.want {
				t.Fatalf("validToken(%q) = %v，期望 %v", c.got, got, c.want)
			}
		})
	}
}

func TestAuthGuardDisabled(t *testing.T) {
	// 令牌为空时应当直接放行，保持本地默认不鉴权
	srv := newGuardedServer("", "Authorization")

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/search", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("未配置令牌时应当放行，实际状态码 %d", rec.Code)
	}
}

func TestAuthGuardRejects(t *testing.T) {
	srv := newGuardedServer(testToken, "Authorization")

	cases := []struct {
		name   string
		header string
		value  string
	}{
		{"没有请求头", "Authorization", ""},
		{"令牌错误", "Authorization", "Bearer wrong-token"},
		{"请求头名不匹配", "X-Other-Header", testToken},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/search", nil)
			if c.value != "" {
				req.Header.Set(c.header, c.value)
			}

			rec := httptest.NewRecorder()
			srv.ServeHTTP(rec, req)

			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("期望 401，实际 %d", rec.Code)
			}

			var resp model.SearchResponse
			if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
				t.Fatalf("响应不是合法 JSON: %v，原文 %s", err, rec.Body.String())
			}
			if resp.Code != 0 || resp.Msg == "" {
				t.Fatalf("期望 code=0 且带 msg，实际 %+v", resp)
			}
		})
	}
}

func TestAuthGuardAccepts(t *testing.T) {
	srv := newGuardedServer(testToken, "Authorization")

	for _, value := range []string{testToken, "Bearer " + testToken} {
		req := httptest.NewRequest(http.MethodPost, "/search", nil)
		req.Header.Set("Authorization", value)

		rec := httptest.NewRecorder()
		srv.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("携带 %q 时应当放行，实际 %d", value, rec.Code)
		}
	}
}

func TestAuthGuardCustomHeader(t *testing.T) {
	srv := newGuardedServer(testToken, "X-Auth-Token")

	req := httptest.NewRequest(http.MethodPost, "/search", nil)
	req.Header.Set("X-Auth-Token", testToken)

	rec := httptest.NewRecorder()
	srv.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("自定义请求头应当生效，实际 %d", rec.Code)
	}
}
