package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"sort"
	"time"

	"github.com/eventide-manafield/manafield-manage/modules/manage-web/internal/coreclient"
	webassets "github.com/eventide-manafield/manafield-manage/modules/manage-web/web"
)

type Server struct {
	core     *coreclient.Client
	version  string
	template *template.Template
	static   http.Handler
}

type pageData struct {
	Version         string
	CoreAvailable   bool
	Modules         []coreclient.Module
	Resources       []coreclient.Resource
	ModuleCount     int
	ResourceCount   int
	CapabilityCount int
}

func New(core *coreclient.Client, version string) (http.Handler, error) {
	if core == nil {
		return nil, fmt.Errorf("Core client is required")
	}

	tmpl, err := template.ParseFS(webassets.FS, "templates/index.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	staticFS, err := fs.Sub(webassets.FS, "static")
	if err != nil {
		return nil, fmt.Errorf("open static assets: %w", err)
	}

	server := &Server{
		core:     core,
		version:  version,
		template: tmpl,
		static:   http.FileServer(http.FS(staticFS)),
	}

	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", server.static))
	mux.HandleFunc("/manafield/health", server.health)
	mux.HandleFunc("/", server.home)

	return mux, nil
}

func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"status":  "ok",
		"service": "manafield-manage-web",
		"version": s.version,
	})
}

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()

	modules, modulesErr := s.core.ListModules(ctx)
	resources, resourcesErr := s.core.ListResources(ctx)
	coreAvailable := modulesErr == nil && resourcesErr == nil

	if modulesErr != nil {
		slog.Warn("Module registry unavailable", "error", modulesErr)
		modules = nil
	}
	if resourcesErr != nil {
		slog.Warn("Resource registry unavailable", "error", resourcesErr)
		resources = nil
	}

	sort.Slice(modules, func(i, j int) bool { return modules[i].Name < modules[j].Name })
	sort.Slice(resources, func(i, j int) bool { return resources[i].Name < resources[j].Name })

	capabilityCount := 0
	for _, resource := range resources {
		capabilityCount += len(resource.Provides.Capabilities)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.template.ExecuteTemplate(w, "index.html", pageData{
		Version:         s.version,
		CoreAvailable:   coreAvailable,
		Modules:         modules,
		Resources:       resources,
		ModuleCount:     len(modules),
		ResourceCount:   len(resources),
		CapabilityCount: capabilityCount,
	}); err != nil {
		slog.Error("render homepage", "error", err)
	}
}
