package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/eventide-manafield/manafield-manage/modules/manage-web/internal/coreclient"
)

func TestHomeRendersInstanceSnapshot(t *testing.T) {
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/modules":
			_, _ = w.Write([]byte(`[{"id":"manafield-reference","name":"Manafield Reference","version":"0.0.1"}]`))
		case "/resources":
			_, _ = w.Write([]byte(`[{"id":"example-postgres","name":"Example PostgreSQL","type":"postgresql","provides":{"capabilities":[{"id":"database.postgresql","version":"1.0.0"}]}}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer core.Close()

	handler, err := New(coreclient.New(core.URL, core.Client()), "test")
	if err != nil {
		t.Fatal(err)
	}

	server := httptest.NewServer(handler)
	defer server.Close()

	resp, err := server.Client().Get(server.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	html := string(body)

	for _, want := range []string{
		"Manafield Manage",
		"Manafield Reference",
		"Example PostgreSQL",
		"database.postgresql",
		"Registry connected",
	} {
		if !strings.Contains(html, want) {
			t.Fatalf("expected %q in homepage", want)
		}
	}
}

func TestHealth(t *testing.T) {
	handler, err := New(coreclient.New("http://127.0.0.1:1", nil), "test")
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodGet, "/manafield/health", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"service":"manafield-manage-web"`) {
		t.Fatalf("unexpected health body: %s", rec.Body.String())
	}
}

func TestPrivateCSSOverrideWithoutRebuild(t *testing.T) {
	cssPath := filepath.Join(t.TempDir(), "my-private-theme.css")
	t.Setenv("MANAFIELD_MANAGE_CUSTOM_CSS_FILE", cssPath)

	handler, err := New(coreclient.New("http://127.0.0.1:1", nil), "test")
	if err != nil { t.Fatal(err) }

	request := func(path string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		return rec
	}

	before := request("/")
	if before.Code != http.StatusOK || strings.Contains(before.Body.String(), "/static/custom.css") {
		t.Fatalf("missing private stylesheet should not be referenced: status=%d", before.Code)
	}
	if got := request("/static/custom.css"); got.Code != http.StatusNotFound {
		t.Fatalf("missing stylesheet should return 404, got %d", got.Code)
	}

	if err := os.WriteFile(cssPath, []byte(":root { --page-bg: #123456; }"), 0600); err != nil { t.Fatal(err) }
	after := request("/")
	if after.Code != http.StatusOK || !strings.Contains(after.Body.String(), "/static/custom.css") {
		t.Fatalf("private stylesheet should load after being created: status=%d", after.Code)
	}
	first := request("/static/custom.css")
	if first.Code != http.StatusOK || !strings.Contains(first.Body.String(), "#123456") {
		t.Fatalf("unexpected custom CSS: status=%d, body=%s", first.Code, first.Body.String())
	}
	if first.Header().Get("Cache-Control") != "no-store" || first.Header().Get("Content-Type") != "text/css; charset=utf-8" {
		t.Fatalf("custom CSS response headers missing: %v", first.Header())
	}

	if err := os.WriteFile(cssPath, []byte(":root { --page-bg: #abcdef; }"), 0600); err != nil { t.Fatal(err) }
	second := request("/static/custom.css")
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), "#abcdef") || strings.Contains(second.Body.String(), "#123456") {
		t.Fatalf("private CSS edit not reflected: status=%d, body=%s", second.Code, second.Body.String())
	}

	method := httptest.NewRecorder()
	handler.ServeHTTP(method, httptest.NewRequest(http.MethodPost, "/static/custom.css", nil))
	if method.Code != http.StatusMethodNotAllowed { t.Fatalf("POST to custom CSS returned %d", method.Code) }
}

func TestPublicStylesheetIsNeutral(t *testing.T) {
	t.Setenv("MANAFIELD_MANAGE_CUSTOM_CSS_FILE", filepath.Join(t.TempDir(), "absent.css"))
	handler, err := New(coreclient.New("http://127.0.0.1:1", nil), "test")
	if err != nil { t.Fatal(err) }
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/static/app.css", nil))
	if rec.Code != http.StatusOK { t.Fatalf("public CSS status=%d", rec.Code) }
	css := rec.Body.String()
	if !strings.Contains(css, "--page-bg: #ffffff") || !strings.Contains(css, "--page-text: #24292f") {
		t.Fatalf("public CSS is not neutral")
	}
	for _, forbidden := range []string{"#ff9b5e", "#ad7aff", "#07050d"} {
		if strings.Contains(css, forbidden) { t.Fatalf("public CSS contains private palette %q", forbidden) }
	}
}
