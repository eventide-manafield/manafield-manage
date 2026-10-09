package web

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/eventide-manafield/manafield-manage/modules/manage-web/internal/sso"
)

var auditIdentityID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

type loginHistoryEntry struct {
	OccurredAt time.Time `json:"occurredAt"`
	Username   string    `json:"username"`
	Outcome    string    `json:"outcome"`
	Reason     string    `json:"reason"`
	PeerIP     string    `json:"peerIp"`
	UserAgent  string    `json:"userAgent"`
	IdentityID string    `json:"identityId"`
}
type auditHistoryPage struct {
	Entries []loginHistoryEntry
	Outcome string
}
type auditService struct {
	roleURL    string
	historyURL string
	roleToken  string
	auditToken string
	client     *http.Client
}

func auditServiceFromEnv() *auditService {
	roleFile := strings.TrimSpace(os.Getenv("MANAFIELD_MANAGE_ROLE_AUTH_CHECK_TOKEN_FILE"))
	auditFile := strings.TrimSpace(os.Getenv("MANAFIELD_MANAGE_ACCOUNT_AUDIT_READ_TOKEN_FILE"))
	if roleFile == "" || auditFile == "" {
		return nil
	}
	return &auditService{
		roleURL:    "http://module-manafield-account-role:8080/manafield/authorization/check",
		historyURL: "http://module-manafield-account-core:8080/manafield/login-history",
		roleToken:  roleFile, auditToken: auditFile,
		client: &http.Client{Timeout: 3 * time.Second},
	}
}

func readServiceToken(path string) (string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	token := strings.TrimSpace(string(b))
	for i := range b {
		b[i] = 0
	}
	if len(token) != 64 {
		return "", fmt.Errorf("invalid internal service token")
	}
	return token, nil
}

func (a *auditService) checkPermission(ctx context.Context, identity string) (bool, error) {
	token, err := readServiceToken(a.roleToken)
	if err != nil {
		return false, err
	}
	payload, _ := json.Marshal(map[string]string{
		"identityId": identity, "permission": "log.audit.read",
	})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.roleURL, bytes.NewReader(payload))
	if err != nil {
		return false, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := a.client.Do(req)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("role authorization unavailable")
	}
	var result struct {
		Allowed bool `json:"allowed"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 2048)).Decode(&result); err != nil {
		return false, err
	}
	return result.Allowed, nil
}

func (a *auditService) list(ctx context.Context, outcome string) ([]loginHistoryEntry, error) {
	token, err := readServiceToken(a.auditToken)
	if err != nil {
		return nil, err
	}
	address, err := url.Parse(a.historyURL)
	if err != nil {
		return nil, err
	}
	q := address.Query()
	q.Set("limit", "50")
	if outcome != "" {
		q.Set("outcome", outcome)
	}
	address.RawQuery = q.Encode()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, address.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login history unavailable")
	}
	var result struct {
		Items []loginHistoryEntry `json:"items"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 65536)).Decode(&result); err != nil {
		return nil, err
	}
	return result.Items, nil
}

// The SSO guard injects a verified Account UUID; a browser-supplied identity
// never selects an authorization principal. Role denies and errors fail shut.
func (s *Server) securityLoginHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		http.Error(w, "Method not allowed", 405)
		return
	}
	identity := sso.IdentityFromContext(r.Context())
	if !auditIdentityID.MatchString(identity) {
		http.Error(w, "Unauthorized", http.StatusUnauthorized)
		return
	}
	if s.audit == nil {
		http.Error(w, "Audit viewer not configured", http.StatusServiceUnavailable)
		return
	}
	outcome := r.URL.Query().Get("outcome")
	if outcome != "" && outcome != "success" && outcome != "failure" && outcome != "blocked" && outcome != "error" {
		http.Error(w, "Invalid outcome", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
	defer cancel()
	allowed, err := s.audit.checkPermission(ctx, identity)
	if err != nil {
		http.Error(w, "Authorization temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	if !allowed {
		http.Error(w, "Forbidden", http.StatusForbidden)
		return
	}
	entries, err := s.audit.list(ctx, outcome)
	if err != nil {
		http.Error(w, "Audit history temporarily unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'self'; base-uri 'none'; frame-ancestors 'none'")
	if err := s.template.ExecuteTemplate(w, "security.html", auditHistoryPage{Entries: entries, Outcome: outcome}); err != nil {
		// An embedded and tested template should not fail.
		http.Error(w, "Template unavailable", 500)
	}
}
