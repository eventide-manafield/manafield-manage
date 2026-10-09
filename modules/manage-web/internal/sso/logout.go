package sso

import (
	"io"
	"net/http"
)

const signedOutCookie = "__Host-mf_manage_signed_out"

// A local logout must not be undone by automatic SSO while Account Core still
// has its own independent browser session. This marker is not an auth token.
func isSignedOut(r *http.Request) bool {
	cookie, err := r.Cookie(signedOutCookie)
	return err == nil && cookie.Value == "1"
}

func markSignedOut(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: signedOutCookie, Value: "1", Path: "/", Secure: true,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 86400,
	})
}

func clearSignedOut(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name: signedOutCookie, Path: "/", Secure: true,
		HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1,
	})
}

// A stable, explicitly logged-out destination avoids an immediate redirect
// through Account Core that would silently log the user straight back in.
func (g *Guard) loggedOut(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	_, _ = io.WriteString(w, `<!doctype html>
<html lang="ko">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width,initial-scale=1">
  <title>로그아웃 · Manafield Manage</title>
  <style>
    :root { color-scheme: light; font-family: system-ui, sans-serif; }
    body { margin: 0; background: #fff; color: #24292f; }
    main { max-width: 520px; margin: min(18vh, 140px) auto; padding: 24px; }
    h1 { font-size: 1.8rem; }
    p { line-height: 1.8; color: #57606a; }
    a { display: inline-block; margin-top: 14px; padding: 9px 16px;
        border: 1px solid #d0d7de; border-radius: 6px; color: #24292f; text-decoration: none; }
    a:hover { background: #f6f8fa; }
  </style>
</head>
<body>
  <main>
    <h1>Manage에서 로그아웃했어.</h1>
    <p>Manafield Account의 중앙 로그인 상태는 그대로 유지돼.
    Manage에 다시 들어가려면 아래 버튼을 눌러 로그인해 줘.</p>
    <a href="/auth/login">다시 로그인</a>
  </main>
</body>
</html>`)
}
