package web

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestManageAuditRequiresScopedServicesAndRoleAllow(t *testing.T) {
	dir := t.TempDir()
	rolePath := filepath.Join(dir, "role.token")
	auditPath := filepath.Join(dir, "audit.token")
	roleToken := strings.Repeat("r", 64)
	auditToken := strings.Repeat("a", 64)
	for path, token := range map[string]string{rolePath: roleToken, auditPath: auditToken} {
		if err := os.WriteFile(path, []byte(token), 0600); err != nil {
			t.Fatal(err)
		}
	}
	allowed := false
	roleCalls, historyCalls := 0, 0
	roleServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		roleCalls++
		if r.Method != "POST" || r.URL.Path != "/check" || r.Header.Get("Authorization") != "Bearer "+roleToken {
			t.Error("Role check has invalid request")
			http.Error(w, "denied", 403)
			return
		}
		var v struct {
			IdentityID string `json:"identityId"`
			Permission string `json:"permission"`
		}
		if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
			t.Error(err)
		}
		if v.IdentityID != "00000000-0000-4000-8000-000000000001" || v.Permission != "log.audit.read" {
			t.Errorf("wrong subject/permission: %+v", v)
		}
		json.NewEncoder(w).Encode(map[string]bool{"allowed": allowed})
	}))
	defer roleServer.Close()
	historyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		historyCalls++
		if r.Method != "GET" || r.Header.Get("Authorization") != "Bearer "+auditToken ||
			r.URL.Query().Get("limit") != "50" {
			t.Errorf("history request not scoped: %s %s", r.Method, r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"items":[{"username":"test","outcome":"failure","reason":"invalid_credentials","peerIp":"198.51.100.21"}]}`))
	}))
	defer historyServer.Close()
	a := &auditService{
		roleURL: roleServer.URL + "/check", historyURL: historyServer.URL + "/history",
		roleToken: rolePath, auditToken: auditPath, client: roleServer.Client(),
	}
	ok, err := a.checkPermission(t.Context(), "00000000-0000-4000-8000-000000000001")
	if err != nil || ok || roleCalls != 1 || historyCalls != 0 {
		t.Fatalf("default deny failed: allowed=%v err=%v role=%d history=%d", ok, err, roleCalls, historyCalls)
	}
	allowed = true
	ok, err = a.checkPermission(t.Context(), "00000000-0000-4000-8000-000000000001")
	if err != nil || !ok {
		t.Fatalf("permission grant ignored: allowed=%v error=%v", ok, err)
	}
	rows, err := a.list(t.Context(), "failure")
	if err != nil || len(rows) != 1 || rows[0].Outcome != "failure" || historyCalls != 1 {
		t.Fatalf("history response failed: rows=%+v error=%v", rows, err)
	}
}

func TestAuditWebRejectsMissingVerifiedIdentity(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest("GET", "https://manage.manafield.studio/security/login-history", nil)
	req.Header.Set("X-Identity-ID", "00000000-0000-4000-8000-000000000001")
	w := httptest.NewRecorder()
	s.securityLoginHistory(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("forged identity header accepted: %d", w.Code)
	}
}
