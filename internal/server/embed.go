package server

import (
	"embed"
	"html/template"
)

//go:embed templates/index.html
var templatesFS embed.FS

var indexTmpl = template.Must(template.ParseFS(templatesFS, "templates/index.html"))
