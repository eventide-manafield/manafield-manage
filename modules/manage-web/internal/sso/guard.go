package sso

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	flowCookie    = "__Host-mf_sso_flow"
	sessionCookie = "__Host-mf_manage_session"
	maxSessions   = 4096
)

type Config struct {
	AuthorizationURL string
	TokenURL         string
	UserInfoURL      string
	ClientID         string
	RedirectURL      string
}

func ConfigFromEnv() (Config, error) {
	c := Config{os.Getenv("MANAFIELD_MANAGE_SSO_AUTHORIZATION_URL"),
		os.Getenv("MANAFIELD_MANAGE_SSO_TOKEN_URL"), os.Getenv("MANAFIELD_MANAGE_SSO_USERINFO_URL"),
		os.Getenv("MANAFIELD_MANAGE_SSO_CLIENT_ID"), os.Getenv("MANAFIELD_MANAGE_SSO_REDIRECT_URL")}
	if c.ClientID == "" {
		return c, errors.New("SSO client ID not configured")
	}
	for _, raw := range []string{c.AuthorizationURL, c.TokenURL, c.UserInfoURL, c.RedirectURL} {
		parsed, err := url.Parse(raw)
		if err != nil || parsed.Host == "" || parsed.User != nil || parsed.Fragment != "" ||
			(parsed.Scheme != "http" && parsed.Scheme != "https") {
			return c, errors.New("incomplete/invalid SSO URL configuration")
		}
	}
	auth, _ := url.Parse(c.AuthorizationURL)
	redirect, _ := url.Parse(c.RedirectURL)
	if auth.Scheme != "https" || redirect.Scheme != "https" || redirect.Path != "/auth/callback" {
		return c, errors.New("public SSO endpoints must be HTTPS and callback /auth/callback")
	}
	return c, nil
}

type flow struct {
	Verifier string
	Until    time.Time
}
type session struct {
	Token string
	Until time.Time
}
type Guard struct {
	cfg      Config
	client   *http.Client
	mu       sync.Mutex
	flows    map[string]flow
	sessions map[[32]byte]session
}

func New(cfg Config, client *http.Client) *Guard {
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Second}
	}
	return &Guard{cfg: cfg, client: client, flows: map[string]flow{}, sessions: map[[32]byte]session{}}
}
func random() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}
func digest(value string) [32]byte { return sha256.Sum256([]byte(value)) }
func (g *Guard) Wrap(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		switch r.URL.Path {
		case "/manafield/health":
			next.ServeHTTP(w, r)
			return
		case "/auth/login":
			g.login(w, r)
			return
		case "/auth/callback":
			g.callback(w, r)
			return
		case "/auth/logout":
			g.logout(w, r)
			return
		}
		if err := g.authenticate(r); err != nil {
			if errors.Is(err, errUnauthenticated) {
				if r.Method == "GET" || r.Method == "HEAD" {
					http.Redirect(w, r, "/auth/login", 303)
				} else {
					http.Error(w, "unauthorized", 401)
				}
			} else {
				http.Error(w, "SSO temporarily unavailable", 503)
			}
			return
		}
		next.ServeHTTP(w, r)
	})
}

var errUnauthenticated = errors.New("not authenticated")

func (g *Guard) login(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	state, err := random()
	if err != nil {
		http.Error(w, "SSO unavailable", 503)
		return
	}
	verifier, err := random()
	if err != nil {
		http.Error(w, "SSO unavailable", 503)
		return
	}
	now := time.Now()
	g.mu.Lock()
	for key, flow := range g.flows {
		if now.After(flow.Until) {
			delete(g.flows, key)
		}
	}
	if len(g.flows) >= 1024 {
		g.mu.Unlock()
		http.Error(w, "SSO temporarily busy", 503)
		return
	}
	g.flows[state] = flow{verifier, now.Add(5 * time.Minute)}
	g.mu.Unlock()
	u, err := url.Parse(g.cfg.AuthorizationURL)
	if err != nil {
		http.Error(w, "SSO config unavailable", 503)
		return
	}
	q := u.Query()
	q.Set("response_type", "code")
	q.Set("client_id", g.cfg.ClientID)
	q.Set("redirect_uri", g.cfg.RedirectURL)
	q.Set("state", state)
	sum := digest(verifier)
	q.Set("code_challenge", base64.RawURLEncoding.EncodeToString(sum[:]))
	q.Set("code_challenge_method", "S256")
	u.RawQuery = q.Encode()
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Value: state, Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode, MaxAge: 300})
	http.Redirect(w, r, u.String(), 303)
}
func (g *Guard) callback(w http.ResponseWriter, r *http.Request) {
	if r.Method != "GET" {
		http.Error(w, "method not allowed", 405)
		return
	}
	state := r.URL.Query().Get("state")
	code := r.URL.Query().Get("code")
	cookie, err := r.Cookie(flowCookie)
	if err != nil || len(state) != 43 || len(code) != 43 || cookie.Value != state {
		http.Error(w, "invalid SSO response", 400)
		return
	}
	g.mu.Lock()
	f, ok := g.flows[state]
	delete(g.flows, state)
	g.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: flowCookie, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	if !ok || time.Now().After(f.Until) {
		http.Error(w, "expired SSO flow", 400)
		return
	}
	fields := url.Values{"grant_type": {"authorization_code"}, "client_id": {g.cfg.ClientID},
		"redirect_uri": {g.cfg.RedirectURL}, "code": {code}, "code_verifier": {f.Verifier}}
	req, err := http.NewRequestWithContext(r.Context(), "POST", g.cfg.TokenURL, strings.NewReader(fields.Encode()))
	if err != nil {
		http.Error(w, "SSO unavailable", 503)
		return
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := g.client.Do(req)
	if err != nil {
		http.Error(w, "SSO unavailable", 503)
		return
	}
	var value struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
		TokenType   string `json:"token_type"`
	}
	if resp.StatusCode == 200 {
		err = json.NewDecoder(io.LimitReader(resp.Body, 4096)).Decode(&value)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 || err != nil || len(value.AccessToken) != 43 || value.TokenType != "Bearer" || value.ExpiresIn <= 0 {
		http.Error(w, "SSO token exchange failed", 401)
		return
	}
	if err := g.userInfo(r.Context(), value.AccessToken); err != nil {
		http.Error(w, "SSO identity validation failed", 401)
		return
	}
	secret, err := random()
	if err != nil {
		http.Error(w, "SSO unavailable", 503)
		return
	}
	seconds := value.ExpiresIn
	if seconds > 900 {
		seconds = 900
	}
	now := time.Now()
	g.mu.Lock()
	for key, item := range g.sessions {
		if now.After(item.Until) {
			delete(g.sessions, key)
		}
	}
	if len(g.sessions) >= maxSessions {
		g.mu.Unlock()
		http.Error(w, "SSO temporarily busy", 503)
		return
	}
	g.sessions[digest(secret)] = session{value.AccessToken, now.Add(time.Duration(seconds) * time.Second)}
	g.mu.Unlock()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: secret, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: seconds})
	http.Redirect(w, r, "/", 303)
}
func (g *Guard) userInfo(ctx context.Context, token string) error {
	req, err := http.NewRequestWithContext(ctx, "GET", g.cfg.UserInfoURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := g.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode == 401 {
		return errUnauthenticated
	}
	if resp.StatusCode != 200 {
		return errors.New("identity provider unavailable")
	}
	var payload struct {
		Subject string `json:"sub"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2048)).Decode(&payload); err != nil {
		return err
	}
	if payload.Subject == "" {
		return errors.New("identity missing")
	}
	return nil
}
func (g *Guard) authenticate(r *http.Request) error {
	cookie, err := r.Cookie(sessionCookie)
	if err != nil || len(cookie.Value) != 43 {
		return errUnauthenticated
	}
	g.mu.Lock()
	s, ok := g.sessions[digest(cookie.Value)]
	g.mu.Unlock()
	if !ok || time.Now().After(s.Until) {
		return errUnauthenticated
	}
	return g.userInfo(r.Context(), s.Token)
}
func (g *Guard) logout(w http.ResponseWriter, r *http.Request) {
	if r.Method != "POST" {
		http.Error(w, "method not allowed", 405)
		return
	}
	if r.Header.Get("Origin") != origin(g.cfg.RedirectURL) {
		http.Error(w, "forbidden", 403)
		return
	}
	if cookie, err := r.Cookie(sessionCookie); err == nil {
		g.mu.Lock()
		delete(g.sessions, digest(cookie.Value))
		g.mu.Unlock()
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Path: "/", Secure: true, HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: -1})
	http.Redirect(w, r, "/auth/login", 303)
}
func origin(raw string) string {
	u, _ := url.Parse(raw)
	if u == nil {
		return ""
	}
	return u.Scheme + "://" + u.Host
}
