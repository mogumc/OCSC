package handler

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"ocs-ai-proxy/internal/logx"
	"ocs-ai-proxy/internal/model"
)

// bearerPrefix 请求头中常见的令牌前缀。
const bearerPrefix = "bearer "

// AuthGuard 用固定令牌保护下游接口，避免私人部署被随意调用产生费用。
// token 为空时直接放行，保持默认「不鉴权」的本地使用体验。
func AuthGuard(token, header string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// OCS 的「测试连接」发的是 HEAD，不带任何令牌。
		// 这类探测请求直接放行，交由 handler 回 200，否则配置界面会误报连接失败。
		if r.Method == http.MethodHead {
			next.ServeHTTP(w, r)
			return
		}

		if !validToken(r.Header.Get(header), token) {
			// 只记录来源与路径，绝不记录令牌本身。
			logx.Warn("鉴权失败: %s %s 来自 %s", r.Method, r.URL.Path, r.RemoteAddr)
			// 这里刻意回 200 而不是 401：OCS 的 handler 只有在拿到响应体时
			// 才能把 msg 显示给用户，回 401 的话界面上只会看到一句笼统的失败。
			writeResponse(w, http.StatusOK, model.SearchResponse{
				Code: 0,
				Msg:  "未授权：请求头 " + header + " 缺少有效的访问令牌",
			})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// validToken 校验令牌，兼容 "Bearer xxx" 与裸 token 两种写法，
// 并使用常量时间比较，避免通过响应耗时推测令牌内容。
func validToken(got, want string) bool {
	got = strings.TrimSpace(got)
	if len(got) >= len(bearerPrefix) && strings.EqualFold(got[:len(bearerPrefix)], bearerPrefix) {
		got = strings.TrimSpace(got[len(bearerPrefix):])
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}
