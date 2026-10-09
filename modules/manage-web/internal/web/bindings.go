package web

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	"github.com/Masterminds/semver/v3"
	"github.com/eventide-manafield/manafield-manage/modules/manage-web/internal/coreclient"
)

// BindingSnapshot is an intentionally small, secret-free projection of the
// active instance. Do not point the UI at a full instance.yaml or release file.
type BindingSnapshot struct {
	Modules map[string]map[string]string `json:"modules"`
}

func readBindingSnapshot(filename string) (*BindingSnapshot, error) {
	if filename == "" {
		return nil, nil
	}
	file, err := os.Open(filename)
	if err != nil {
		return nil, fmt.Errorf("read binding snapshot: %w", err)
	}
	defer file.Close()

	var data BindingSnapshot
	decoder := json.NewDecoder(io.LimitReader(file, 1<<20+1))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&data); err != nil {
		return nil, fmt.Errorf("decode binding snapshot: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("binding snapshot must contain one JSON object")
	}
	if data.Modules == nil {
		return nil, fmt.Errorf("binding snapshot must contain modules")
	}
	return &data, nil
}

type moduleSummary struct {
	Module         coreclient.Module
	DetailURL      string
	RequiredBound  int
	RequiredTotal  int
	BindingsKnown  bool
	Warnings       []string
}

type provider struct {
	ID      string
	Kind    string
	Version string
}

type requirementDetail struct {
	Slot          string
	CapabilityID  string
	VersionRange  string
	Optional      bool
	Bound         bool
	Target        string
	ProviderKind  string
	FoundVersion  string
	Status        string
	Warning       bool
	Candidates    []provider
}

type moduleDetail struct {
	Module           coreclient.Module
	Requirements     []requirementDetail
	RequiredCount    int
	OptionalCount    int
	RequiredBound    int
	BindingsKnown    bool
	Warnings         []string
}

func makeModuleViews(
	modules []coreclient.Module, resources []coreclient.Resource, snapshot *BindingSnapshot,
) ([]moduleSummary, map[string]moduleDetail) {
	summaries := make([]moduleSummary, 0, len(modules))
	details := make(map[string]moduleDetail, len(modules))
	for _, module := range modules {
		slots := module.Requires.Capabilities
		names := make([]string, 0, len(slots))
		for slot := range slots {
			names = append(names, slot)
		}
		sort.Strings(names)

		detail := moduleDetail{Module: module}
		bindings := map[string]string{}
		if snapshot != nil {
			bindings = snapshot.Modules[module.ID]
			detail.BindingsKnown = true
		}

		for _, slot := range names {
			req := slots[slot]
			item := requirementDetail{
				Slot: slot, CapabilityID: req.ID,
				VersionRange: req.Version, Optional: req.Optional,
				Candidates: findProviders(req.ID, modules, resources),
			}
			if req.Optional {
				detail.OptionalCount++
			} else {
				detail.RequiredCount++
			}

			if snapshot == nil {
				item.Status = "바인딩 정보 없음"
			} else {
				item.Target = strings.TrimSpace(bindings[slot])
				item.Bound = item.Target != ""
				if item.Bound && !req.Optional {
					detail.RequiredBound++
				}
				if !item.Bound {
					item.Status = "미연결"
					if len(item.Candidates) == 0 {
						item.Status = "Provider 없음"
						item.Warning = true
					}
				} else {
					item.Status, item.ProviderKind, item.FoundVersion = validateBinding(item.Target, req, modules, resources)
					item.Warning = item.Status != "정상"
				}
			}
			if item.Warning {
				detail.Warnings = appendUnique(detail.Warnings, item.Status)
			}
			detail.Requirements = append(detail.Requirements, item)
		}
		summaries = append(summaries, moduleSummary{
			Module: module,
			DetailURL: "/modules/" + module.ID,
			RequiredBound: detail.RequiredBound, RequiredTotal: detail.RequiredCount,
			BindingsKnown: detail.BindingsKnown || detail.RequiredCount == 0,
			Warnings: detail.Warnings,
		})
		details[module.ID] = detail
	}
	return summaries, details
}

func appendUnique(items []string, value string) []string {
	for _, item := range items {
		if item == value {
			return items
		}
	}
	return append(items, value)
}

func findProviders(capabilityID string, modules []coreclient.Module, resources []coreclient.Resource) []provider {
	candidates := make([]provider, 0)
	for _, mod := range modules {
		for _, cap := range mod.Provides.Capabilities {
			if cap.ID == capabilityID {
				candidates = append(candidates, provider{ID: mod.ID, Kind: "Module", Version: cap.Version})
			}
		}
	}
	for _, resource := range resources {
		for _, cap := range resource.Provides.Capabilities {
			if cap.ID == capabilityID {
				candidates = append(candidates, provider{ID: resource.ID, Kind: "Resource", Version: cap.Version})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		if candidates[i].ID != candidates[j].ID {
			return candidates[i].ID < candidates[j].ID
		}
		return candidates[i].Kind < candidates[j].Kind
	})
	return candidates
}

func validateBinding(target string, req coreclient.Requirement, modules []coreclient.Module, resources []coreclient.Resource) (string, string, string) {
	var capabilities []coreclient.Capability
	kind := ""
	for _, m := range modules {
		if m.ID == target {
			kind, capabilities = "Module", m.Provides.Capabilities
			break
		}
	}
	if kind == "" {
		for _, r := range resources {
			if r.ID == target {
				kind, capabilities = "Resource", r.Provides.Capabilities
				break
			}
		}
	}
	if kind == "" {
		return "대상 없음", "", ""
	}
	for _, cap := range capabilities {
		if cap.ID != req.ID {
			continue
		}
		constraint, err := semver.NewConstraint(req.Version)
		if err != nil {
			return "버전 판정 불가", kind, cap.Version
		}
		version, err := semver.NewVersion(cap.Version)
		if err != nil {
			return "버전 판정 불가", kind, cap.Version
		}
		if !constraint.Check(version) {
			return "버전 불일치", kind, cap.Version
		}
		return "정상", kind, cap.Version
	}
	return "Capability 없음", kind, ""
}
