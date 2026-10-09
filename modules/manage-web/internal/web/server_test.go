package web

import (
	"io"
	"net/http"
	"net/http/httptest"
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
