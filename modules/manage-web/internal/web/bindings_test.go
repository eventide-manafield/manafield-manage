package web

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/eventide-manafield/manafield-manage/modules/manage-web/internal/coreclient"
)

func sampleModules() []coreclient.Module {
	return []coreclient.Module{
		{
			ID: "consumer", Name: "Consumer", Version: "1.0.0",
			Requires: coreclient.RequirementSet{Capabilities: map[string]coreclient.Requirement{
				"identity": {ID: "manafield.identity", Version: "^1.0.0"},
				"state": {ID: "database.postgresql", Version: "^1.0.0"},
				"cache": {ID: "cache.redis", Version: "^1.0.0", Optional: true},
			}},
		},
		{
			ID: "identity-module", Name: "Identity", Version: "4.0.0",
			Provides: coreclient.CapabilitySet{Capabilities: []coreclient.Capability{
				{ID: "manafield.identity", Version: "1.2.0"},
			}},
		},
	}
}

func sampleResources() []coreclient.Resource {
	return []coreclient.Resource{
		{ID: "main-postgres", Name: "PostgreSQL", Provides: coreclient.CapabilitySet{
			Capabilities: []coreclient.Capability{{ID: "database.postgresql", Version: "1.0.3"}},
		}},
	}
}

func TestRequiredBindingTotalsAndDiagnostics(t *testing.T) {
	snapshot := &BindingSnapshot{Modules: map[string]map[string]string{
		"consumer": {"identity": "identity-module", "state": "main-postgres"},
	}}
	views, all := makeModuleViews(sampleModules(), sampleResources(), snapshot)
	if len(views) != 2 {
		t.Fatalf("view count: %d", len(views))
	}
	var v moduleSummary
	for _, item := range views {
		if item.Module.ID == "consumer" { v = item; break }
	}
	if !v.BindingsKnown || v.RequiredTotal != 2 || v.RequiredBound != 2 {
		t.Fatalf("wrong required binding totals: %+v", v)
	}
	detail := all["consumer"]
	if detail.RequiredCount != 2 || detail.OptionalCount != 1 || len(detail.Requirements) != 3 {
		t.Fatalf("wrong requirement totals: %+v", detail)
	}
	if len(detail.Warnings) != 1 || detail.Warnings[0] != "Provider 없음" {
		t.Fatalf("missing provider warning for optional cache: %+v", detail.Warnings)
	}
	if detail.Requirements[1].Slot != "identity" || detail.Requirements[2].Slot != "state" {
		t.Fatalf("requirement order not stable: %+v", detail.Requirements)
	}
	if detail.Requirements[2].Status != "정상" {
		t.Fatalf("valid PostgreSQL requirement incorrectly rejected: %+v", detail.Requirements[2])
	}
}

func TestBindingIsCountedEvenIfVersionMismatches(t *testing.T) {
	resources := sampleResources()
	resources[0].Provides.Capabilities[0].Version = "2.0.0"
	snapshot := &BindingSnapshot{Modules: map[string]map[string]string{
		"consumer": {"identity": "identity-module", "state": "main-postgres"},
	}}
	views, details := makeModuleViews(sampleModules(), resources, snapshot)
	if views[0].RequiredBound != 2 || views[0].RequiredTotal != 2 {
		t.Fatalf("mismatch changed BOUND count: %+v", views[0])
	}
	if details["consumer"].Requirements[2].Status != "버전 불일치" {
		t.Fatalf("no mismatch diagnostic: %+v", details["consumer"].Requirements[2])
	}
	snapshot.Modules["consumer"]["state"] = "does-not-exist"
	views, details = makeModuleViews(sampleModules(), resources, snapshot)
	if views[0].RequiredBound != 2 || details["consumer"].Requirements[2].Status != "대상 없음" {
		t.Fatalf("missing provider must not change BOUND count: %+v", details["consumer"])
	}
}

func TestUnknownSnapshotIsNotMisrepresentedAsZeroBound(t *testing.T) {
	views, details := makeModuleViews(sampleModules(), sampleResources(), nil)
	if views[0].BindingsKnown || details["consumer"].BindingsKnown {
		t.Fatal("unknown binding snapshot must not be reported as known")
	}
	if views[0].RequiredTotal != 2 {
		t.Fatalf("required count must come from registry: %d", views[0].RequiredTotal)
	}
	if !views[1].BindingsKnown || views[1].RequiredTotal != 0 {
		t.Fatalf("module with zero required slots should be 0/0: %+v", views[1])
	}
}

func TestBindingSnapshotIsRestrictedToPublicIdentifiers(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "bindings.json")
	good := []byte(`{"modules":{"consumer":{"state":"main-postgres"}}}`)
	if err := os.WriteFile(filename, good, 0600); err != nil { t.Fatal(err) }
	snapshot, err := readBindingSnapshot(filename)
	if err != nil || snapshot.Modules["consumer"]["state"] != "main-postgres" {
		t.Fatalf("expected a valid projection: %v %+v", err, snapshot)
	}
	for _, body := range []string{
		`{"modules":{"consumer":{"state":"main-postgres"}},"token":"sensitive"}`,
		`{"modules":{"consumer":{"state":"main-postgres"}}}{}`,
		`{"modules":null}`,
	} {
		if err := os.WriteFile(filename, []byte(body), 0600); err != nil { t.Fatal(err) }
		if _, err := readBindingSnapshot(filename); err == nil {
			t.Fatalf("should reject invalid snapshot: %s", body)
		}
	}
}

func TestUnboundSlotAndCapabilityMismatch(t *testing.T) {
	snapshot := &BindingSnapshot{Modules: map[string]map[string]string{
		"consumer": {"state": "identity-module"},
	}}
	_, details := makeModuleViews(sampleModules(), sampleResources(), snapshot)
	d := details["consumer"]
	if d.RequiredBound != 1 {
		t.Fatalf("expected one required binding, got %d", d.RequiredBound)
	}
	if d.Requirements[1].Status != "미연결" {
		t.Fatalf("identity should be unbound: %+v", d.Requirements[1])
	}
	if d.Requirements[2].Status != "Capability 없음" {
		t.Fatalf("invalid target capability not detected: %+v", d.Requirements[2])
	}
}

func TestModuleDetailPathIsEscaped(t *testing.T) {
	if got := moduleDetailURL("custom/my module"); got != "/modules/custom%2Fmy%20module" {
		t.Fatalf("escaped path = %q", got)
	}
}
