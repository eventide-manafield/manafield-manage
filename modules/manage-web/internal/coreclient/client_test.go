package coreclient

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestListModulesAndResources(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	defer server.Close()

	client := New(server.URL, server.Client())

	modules, err := client.ListModules(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(modules) != 1 || modules[0].ID != "manafield-reference" {
		t.Fatalf("unexpected modules: %#v", modules)
	}

	resources, err := client.ListResources(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(resources) != 1 || resources[0].ID != "example-postgres" {
		t.Fatalf("unexpected resources: %#v", resources)
	}
	if got := resources[0].Provides.Capabilities[0].ID; got != "database.postgresql" {
		t.Fatalf("unexpected capability: %s", got)
	}
}
