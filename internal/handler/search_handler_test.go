package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"ocs-ai-proxy/internal/model"
)

// newHeadTestHandler 创建一个不接上游的处理器，用于验证请求方法相关的分支。
// 走到上游之前的路径都不需要 proxy，传 nil 即可。
func newHeadTestHandler() *SearchHandler {
	return NewSearchHandler(nil, nil, 0)
}

func TestServeHTTPMethodHandling(t *testing.T) {
	h := newHeadTestHandler()

	t.Run("HEAD 探测回 200", func(t *testing.T) {
		// OCS 的「测试连接」就是 HEAD /search?t=
		req := httptest.NewRequest(http.MethodHead, "/search?t=", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("HEAD 应当回 200，实际 %d", rec.Code)
		}
	})

	t.Run("GET 仍不接受", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/search", nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusMethodNotAllowed {
			t.Fatalf("GET 应当回 405，实际 %d", rec.Code)
		}
	})

	t.Run("POST 题目为空回 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader(`{"title":"   "}`))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("空题目应当回 400，实际 %d", rec.Code)
		}

		var resp model.SearchResponse
		if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
			t.Fatalf("响应不是合法 JSON: %v", err)
		}
		if resp.Code != 0 || resp.Msg == "" {
			t.Fatalf("期望 code=0 且带 msg，实际 %+v", resp)
		}
	})

	t.Run("POST 请求体坏掉回 400", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/search", strings.NewReader("not-json"))
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("非法 JSON 应当回 400，实际 %d", rec.Code)
		}
	})
}

func TestCleanOptions(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"去掉空行与行首尾空白", "A. 1\n\n   B. 2  \nC. 3", "A. 1\nB. 2\nC. 3"},
		{"全是空白时得到空串", "  \n\t\n ", ""},
		{"空输入得到空串", "", ""},
		{"单行原样保留", "A. 1", "A. 1"},
		{"保留选项内部空格", "A. 新  中国\nB. 其他", "A. 新  中国\nB. 其他"},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := cleanOptions(c.in); got != c.want {
				t.Fatalf("cleanOptions(%q) = %q，期望 %q", c.in, got, c.want)
			}
		})
	}
}
