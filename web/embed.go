package web

import "embed"

//go:embed templates/* static/css/* static/js/*
var TemplateFS embed.FS
