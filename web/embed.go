package web

import "embed"

//go:embed templates/* static/*
var TemplateFS embed.FS
