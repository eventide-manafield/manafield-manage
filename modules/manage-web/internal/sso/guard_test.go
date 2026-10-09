package sso

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestUnauthenticatedRequestsRequireLogin(t *testing.T) {
	g := New(Config{AuthorizationURL: "https://account.test/account/oauth/authorize", TokenURL: "http://unused/token", UserInfoURL: "http://unused/userinfo", ClientID: "manage", RedirectURL: "https://manage.test/auth/callback"}, nil)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); w.Write([]byte("PRIVATE_CONTENT")) })
	h := g.Wrap(next)
	for _, path := range []string{"/", "/modules/example", "/static/app.css", "/static/custom.css"} {
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("GET", "https://manage.test"+path, nil))
		if w.Code != 303 || w.Header().Get("Location") != "/auth/login" || strings.Contains(w.Body.String(), "PRIVATE_CONTENT") {
			t.Fatalf("path %q leaked: %d %q", path, w.Code, w.Body.String())
		}
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("POST", "https://manage.test/modules/example", nil))
	if w.Code != 401 {
		t.Fatalf("anonymous write %d", w.Code)
	}
	w = httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "https://manage.test/manafield/health", nil))
	if w.Code != 200 {
		t.Fatalf("health %d", w.Code)
	}
}
func TestPKCELoginCallbackAndRevocation(t *testing.T) {
	accessToken := strings.Repeat("t", 43)
	active := true
	issuer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			r.ParseForm()
			if r.Method != "POST" || len(r.PostForm.Get("code_verifier")) != 43 || r.PostForm.Get("code") != strings.Repeat("c", 43) {
				t.Errorf("invalid token exchange")
				http.Error(w, "invalid", 400)
				return
			}
			json.NewEncoder(w).Encode(map[string]any{"access_token": accessToken, "token_type": "Bearer", "expires_in": 900})
		case "/userinfo":
			if !active {
				http.Error(w, "invalid", 401)
				return
			}
			if r.Header.Get("Authorization") != "Bearer "+accessToken {
				http.Error(w, "invalid", 401)
				return
			}
			json.NewEncoder(w).Encode(map[string]string{"sub": "00000000-0000-4000-8000-000000000001"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer issuer.Close()
	g := New(Config{AuthorizationURL: "https://account.test/account/oauth/authorize", TokenURL: issuer.URL + "/token", UserInfoURL: issuer.URL + "/userinfo", ClientID: "manage", RedirectURL: "https://manage.test/auth/callback"}, issuer.Client())
	h := g.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("PRIVATE_CONTENT")) }))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest("GET", "https://manage.test/auth/login", nil))
	if w.Code != 303 {
		t.Fatalf("login %d", w.Code)
	}
	u, err := url.Parse(w.Header().Get("Location"))
	if err != nil {
		t.Fatal(err)
	}
	if u.Query().Get("code_challenge_method") != "S256" || len(u.Query().Get("code_challenge")) != 43 {
		t.Fatal("no PKCE S256")
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == flowCookie {
			cookie = c
		}
	}
	if cookie == nil {
		t.Fatal("missing SSO flow cookie")
	}
	state := u.Query().Get("state")
	callback := "https://manage.test/auth/callback?state=" + url.QueryEscape(state) + "&code=" + strings.Repeat("c", 43)
	req := httptest.NewRequest("GET", callback, nil)
	req.AddCookie(cookie)
	done := httptest.NewRecorder()
	h.ServeHTTP(done, req)
	if done.Code != 303 || done.Header().Get("Location") != "/" {
		t.Fatalf("callback %d %q", done.Code, done.Body.String())
	}
	var sessionCookie *http.Cookie
	for _, c := range done.Result().Cookies() {
		if c.Name == "__Host-mf_manage_session" {
			sessionCookie = c
		}
	}
	if sessionCookie == nil || !sessionCookie.HttpOnly || !sessionCookie.Secure {
		t.Fatal("insecure session cookie")
	}
	private := httptest.NewRequest("GET", "https://manage.test/", nil)
	private.AddCookie(sessionCookie)
	got := httptest.NewRecorder()
	h.ServeHTTP(got, private)
	if got.Code != 200 || !strings.Contains(got.Body.String(), "PRIVATE_CONTENT") {
		t.Fatalf("logged in %d", got.Code)
	}
	active = false
	denied := httptest.NewRecorder()
	h.ServeHTTP(denied, private)
	if denied.Code != 303 {
		t.Fatalf("revoked access %d", denied.Code)
	}
}
func TestForgedCallbackAndLogoutCSRF(t *testing.T) {
	g := New(Config{AuthorizationURL: "https://account.test/account/oauth/authorize", ClientID: "manage", RedirectURL: "https://manage.test/auth/callback"}, nil)
	h := g.Wrap(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	bad := httptest.NewRecorder()
	h.ServeHTTP(bad, httptest.NewRequest("GET", "https://manage.test/auth/callback?state=x&code=y", nil))
	if bad.Code != 400 {
		t.Fatalf("forged callback %d", bad.Code)
	}
	req := httptest.NewRequest("POST", "https://manage.test/auth/logout", nil)
	req.Header.Set("Origin", "https://evil.test")
	denied := httptest.NewRecorder()
	h.ServeHTTP(denied, req)
	if denied.Code != 403 {
		t.Fatalf("logout CSRF %d", denied.Code)
	}
	opaque := httptest.NewRequest("POST", "https://manage.test/auth/logout", nil)
	opaque.Header.Set("Origin", "null")
	opaque.Header.Set("Sec-Fetch-Site", "same-origin")
	accepted := httptest.NewRecorder()
	h.ServeHTTP(accepted, opaque)
	if accepted.Code != 303 {
		t.Fatalf("same-origin opaque browser logout denied: %d", accepted.Code)
	}
}

func TestMissingSSOConfigFailsClosed(t *testing.T) {
	for _, env := range []string{
		"MANAFIELD_MANAGE_SSO_AUTHORIZATION_URL",
		"MANAFIELD_MANAGE_SSO_TOKEN_URL",
		"MANAFIELD_MANAGE_SSO_USERINFO_URL",
		"MANAFIELD_MANAGE_SSO_CLIENT_ID",
		"MANAFIELD_MANAGE_SSO_REDIRECT_URL",
	} {
		t.Setenv(env, "")
	}
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("Manage must refuse startup without SSO configuration")
	}
	t.Setenv("MANAFIELD_MANAGE_SSO_AUTHORIZATION_URL", "https://manafield.studio/account/oauth/authorize")
	t.Setenv("MANAFIELD_MANAGE_SSO_TOKEN_URL", "http://module-manafield-account-core:8080/account/oauth/token")
	t.Setenv("MANAFIELD_MANAGE_SSO_USERINFO_URL", "http://module-manafield-account-core:8080/account/oauth/userinfo")
	t.Setenv("MANAFIELD_MANAGE_SSO_CLIENT_ID", "manafield-manage-web")
	t.Setenv("MANAFIELD_MANAGE_SSO_REDIRECT_URL", "https://manage.manafield.studio/auth/callback")
	if _, err := ConfigFromEnv(); err != nil {
		t.Fatalf("valid first-party deployment SSO config rejected: %v", err)
	}
}

func TestLogoutDoesNotSilentlyRestartAccountSSO(t *testing.T) {
	g := New(Config{
		AuthorizationURL: "https://account.test/account/oauth/authorize",
		ClientID: "manage",
		RedirectURL: "https://manage.test/auth/callback",
	}, nil)
	h := g.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("PRIVATE_CONTENT"))
	}))

	secret := strings.Repeat("s", 43)
	g.sessions[digest(secret)] = session{Token: strings.Repeat("t", 43), Until: time.Now().Add(time.Minute)}
	req := httptest.NewRequest(http.MethodPost, "https://manage.test/auth/logout", nil)
	req.Header.Set("Origin", "https://manage.test")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	req.AddCookie(&http.Cookie{Name: sessionCookie, Value: secret})
	result := httptest.NewRecorder()
	h.ServeHTTP(result, req)
	if result.Code != http.StatusSeeOther || result.Header().Get("Location") != "/auth/logged-out" {
		t.Fatalf("logout must end on a stable page, got %d %q", result.Code, result.Header().Get("Location"))
	}
	if len(g.sessions) != 0 {
		t.Fatal("Manage session was not revoked server-side")
	}

	var hold *http.Cookie
	var erased bool
	for _, cookie := range result.Result().Cookies() {
		if cookie.Name == sessionCookie && cookie.MaxAge < 0 {
			erased = true
		}
		if cookie.Name == signedOutCookie {
			hold = cookie
		}
	}
	if !erased || hold == nil || hold.Value != "1" || !hold.Secure || !hold.HttpOnly || hold.SameSite != http.SameSiteLaxMode {
		t.Fatalf("logout cookies are incomplete: %v", result.Result().Cookies())
	}
	// A stale Manage cookie plus the signed-out marker must still stay logged out.
	private := httptest.NewRequest(http.MethodGet, "https://manage.test/", nil)
	private.AddCookie(&http.Cookie{Name: sessionCookie, Value: secret})
	private.AddCookie(hold)
	blocked := httptest.NewRecorder()
	h.ServeHTTP(blocked, private)
	if blocked.Code != http.StatusSeeOther || blocked.Header().Get("Location") != "/auth/logged-out" {
		t.Fatalf("silent SSO restarted after logout: %d %s", blocked.Code, blocked.Header().Get("Location"))
	}
	landing := httptest.NewRecorder()
	h.ServeHTTP(landing, httptest.NewRequest(http.MethodGet, "https://manage.test/auth/logged-out", nil))
	if landing.Code != http.StatusOK || !strings.Contains(landing.Body.String(), "다시 로그인") ||
		strings.Contains(landing.Body.String(), "PRIVATE_CONTENT") {
		t.Fatalf("logged-out landing: %d %s", landing.Code, landing.Body.String())
	}
	// Explicit click on re-login is allowed to clear the hold and start PKCE.
	login := httptest.NewRecorder()
	logReq := httptest.NewRequest(http.MethodGet, "https://manage.test/auth/login", nil)
	logReq.AddCookie(hold)
	h.ServeHTTP(login, logReq)
	if login.Code != http.StatusSeeOther || !strings.HasPrefix(login.Header().Get("Location"), g.cfg.AuthorizationURL+"?") {
		t.Fatalf("explicit SSO login not started: %d %q", login.Code, login.Header().Get("Location"))
	}
	var cleared bool
	for _, cookie := range login.Result().Cookies() {
		if cookie.Name == signedOutCookie && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("explicit login failed to clear signed-out marker")
	}
}

func TestLogoutOriginValidationForBrowserForms(t *testing.T) {
	g := New(Config{RedirectURL: "https://manage.test/auth/callback"}, nil)
	h := g.Wrap(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	for _, tc := range []struct {
		name, origin, fetchSite string
		status int
	}{
		{"same-origin", "https://manage.test", "same-origin", http.StatusSeeOther},
		{"opaque-same-origin", "null", "same-origin", http.StatusSeeOther},
		{"omitted-origin-same-origin", "", "same-origin", http.StatusSeeOther},
		{"foreign-origin-with-forged-metadata", "https://evil.test", "same-origin", http.StatusForbidden},
		{"opaque-same-site", "null", "same-site", http.StatusForbidden},
		{"cross-site", "https://manage.test", "cross-site", http.StatusForbidden},
		{"unverified-missing-origin", "", "", http.StatusForbidden},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "https://manage.test/auth/logout", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.fetchSite != "" {
				req.Header.Set("Sec-Fetch-Site", tc.fetchSite)
			}
			result := httptest.NewRecorder()
			h.ServeHTTP(result, req)
			if result.Code != tc.status {
				t.Fatalf("logout %d, want %d", result.Code, tc.status)
			}
		})
	}
}
