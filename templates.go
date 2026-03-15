package main

import (
	"embed"
	"html/template"
)

//go:embed templates/index.tmpl
var templateFS embed.FS

func mustParseTemplates() *template.Template {
	return template.Must(template.ParseFS(templateFS, "templates/index.tmpl"))
}
