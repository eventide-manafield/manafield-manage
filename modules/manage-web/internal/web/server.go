package web

import (
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/eventide-manafield/manafield-manage/modules/manage-web/internal/coreclient"
	webassets "github.com/eventide-manafield/manafield-manage/modules/manage-web/web"
)

type Server struct {
	core          *coreclient.Client
	version       string
	template      *template.Template
	static        http.Handler
	customCSSFile string
	bindingsFile  string
}

type pageData struct {
	Version         string
	CustomCSS       bool
	CoreAvailable   bool
	Modules         []moduleSummary
	BindingsError   bool
	Resources       []coreclient.Resource
	ModuleCount     int
	ResourceCount   int
	CapabilityCount int
}

func New(core *coreclient.Client, version string) (http.Handler, error) {
	if core == nil {
		return nil, fmt.Errorf("Core client is required")
	}

	tmpl, err := template.ParseFS(webassets.FS, "templates/index.html", "templates/module.html")
	if err != nil {
		return nil, fmt.Errorf("parse templates: %w", err)
	}

	staticFS, err := fs.Sub(webassets.FS, "static")
	if err != nil {
		return nil, fmt.Errorf("open static assets: %w", err)
	}

	customCSSFile := strings.TrimSpace(os.Getenv("MANAFIELD_MANAGE_CUSTOM_CSS_FILE"))
	if customCSSFile == "" {
		customCSSFile = "custom.css"
	}

	server := &Server{
		core:          core,
		version:       version,
		template:      tmpl,
		static:        http.FileServer(http.FS(staticFS)),
		customCSSFile: customCSSFile,
		bindingsFile:  strings.TrimSpace(os.Getenv("MANAFIELD_MANAGE_BINDINGS_FILE")),
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/static/custom.css", server.customCSS)
	mux.Handle("/static/", http.StripPrefix("/static/", server.static))
	mux.HandleFunc("/manafield/health", server.health)
	mux.HandleFunc("GET /modules/{id}", server.modulePage)
	mux.HandleFunc("/", server.home)

	return mux, nil
}

// customCSS streams a privately managed stylesheet without embedding it into
// the public binary. A browser refresh picks up file edits without a rebuild.
func (s *Server) customCSS(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}
	file, err := os.Open(s.customCSSFile)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	http.ServeContent(w, r, "custom.css", time.Time{}, file)
}

func (s *Server) hasCustomCSS() bool {
	info, err := os.Stat(s.customCSSFile)
	return err == nil && info.Mode().IsRegular()
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

	snapshot, bindingErr := readBindingSnapshot(s.bindingsFile)
	if bindingErr != nil {
		slog.Warn("Binding snapshot unavailable", "error", bindingErr)
	}
	moduleViews, _ := makeModuleViews(modules, resources, snapshot)

	capabilityCount := 0
	for _, resource := range resources {
		capabilityCount += len(resource.Provides.Capabilities)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := s.template.ExecuteTemplate(w, "index.html", pageData{
		Version:         s.version,
		CustomCSS:       s.hasCustomCSS(),
		CoreAvailable:   coreAvailable,
		Modules:         moduleViews,
		BindingsError:   bindingErr != nil,
		Resources:       resources,
		ModuleCount:     len(modules),
		ResourceCount:   len(resources),
		CapabilityCount: capabilityCount,
	}); err != nil {
		slog.Error("render homepage", "error", err)
	}
}

type modulePageData struct {
	Version       string
	CustomCSS     bool
	Detail        moduleDetail
	BackURL       string
	SnapshotError bool
}

func (s *Server) modulePage(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	modules, modErr := s.core.ListModules(ctx)
	resources, resErr := s.core.ListResources(ctx)
	if modErr != nil || resErr != nil {
		http.Error(w, "Module registry unavailable", http.StatusServiceUnavailable)
		return
	}
	var found bool
	for _, mod := range modules {
		if mod.ID == id {
			found = true
			break
		}
	}
	if !found {
		http.NotFound(w, r)
		return
	}
	snapshot, err := readBindingSnapshot(s.bindingsFile)
	if err != nil {
		slog.Warn("Binding snapshot unavailable", "error", err)
	}
	_, details := makeModuleViews(modules, resources, snapshot)
	detail := details[id]
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if err := s.template.ExecuteTemplate(w, "module.html", modulePageData{
		Version: s.version, CustomCSS: s.hasCustomCSS(),
		Detail: detail, BackURL: "/", SnapshotError: err != nil,
	}); err != nil {
		slog.Error("render module details", "error", err)
	}
}

// Escape dynamic module identifiers as URL path segments, not arbitrary links.
func moduleDetailURL(id string) string {
	return "/modules/" + url.PathEscape(id)
}
