package web

import "embed"

// FS contains the homepage templates and static assets.
//
//go:embed templates/*.html static/*
var FS embed.FS
