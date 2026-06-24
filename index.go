package actions

import (
	"bytes"
	"fmt"
	"html/template"
	"net/http"
	"strings"
)

// indexPageTemplate renders the /_actions HTML index. The title is driven by the
// registry's configured OpenAPI info title.
//
//nolint:gochecknoglobals // a compile-time-parsed template is an intentional package global
var indexPageTemplate = template.Must(template.New("index").Parse(`<!DOCTYPE html>
<html lang="en">
<head><meta charset="utf-8"><title>{{.Title}} Actions</title></head>
<body>
<h1>{{.Title}} Actions</h1>
<p>{{len .Rows}} registered action(s). Machine-readable contract: <a href="openapi.json">openapi.json</a>.</p>
{{range .Rows}}<section>
<h2>{{.Method}} {{.Path}}</h2>
<p><strong>{{.ID}}</strong> &mdash; {{.Summary}}</p>
{{if .Tags}}<p>Tags: {{range .Tags}}<code>{{.}}</code> {{end}}</p>{{end}}
<table border="1"><tr><th>Status</th><th>Description</th></tr>
{{range .Statuses}}<tr><td>{{.Code}}</td><td>{{.Description}}</td></tr>{{end}}
</table>
</section>{{end}}
</body>
</html>
`))

// indexRow is one action's data for the index template.
type indexRow struct {
	ID       string
	Method   string
	Path     string
	Summary  string
	Tags     []string
	Statuses []StatusDoc
}

// indexData is the top-level payload passed to the index template.
type indexData struct {
	Title string
	Rows  []indexRow
}

// buildIndex pre-renders the HTML and Markdown _actions index.
func (r *Registry) buildIndex() {
	rows := make([]indexRow, len(r.actions))
	for i, a := range r.actions {
		rows[i] = indexRow{
			ID: a.id, Method: a.method, Path: a.path,
			Summary: a.summary, Tags: a.tags, Statuses: a.statuses,
		}
	}

	var html bytes.Buffer
	if err := indexPageTemplate.Execute(&html, indexData{Title: r.info.title, Rows: rows}); err != nil {
		panic(fmt.Errorf("actions: render HTML index: %w", err))
	}
	r.indexHTML = html.Bytes()
	r.indexMarkdown = renderMarkdownIndex(r.info.title, rows)
}

// renderMarkdownIndex builds the Markdown rendering of the action index.
func renderMarkdownIndex(title string, rows []indexRow) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s Actions\n\n%d registered action(s).\n", title, len(rows))
	for _, row := range rows {
		fmt.Fprintf(&b, "\n## %s %s\n\n", row.Method, row.Path)
		fmt.Fprintf(&b, "**%s** — %s\n\n", row.ID, row.Summary)
		if len(row.Tags) > 0 {
			fmt.Fprintf(&b, "Tags: %s\n\n", strings.Join(row.Tags, ", "))
		}
		b.WriteString("| Status | Description |\n| --- | --- |\n")
		for _, sd := range row.Statuses {
			fmt.Fprintf(&b, "| %d | %s |\n", sd.Code, sd.Description)
		}
	}
	return []byte(b.String())
}

// serveIndex serves the /_actions index, content-negotiating HTML and Markdown.
func (r *Registry) serveIndex(w http.ResponseWriter, req *http.Request) {
	if acceptsMarkdown(req) {
		w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(r.indexMarkdown)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(r.indexHTML)
}

// acceptsMarkdown reports whether the request's Accept header asks for a
// Markdown or plain-text rendering.
func acceptsMarkdown(r *http.Request) bool {
	accept := r.Header.Get("Accept")
	for _, part := range strings.Split(accept, ",") {
		media := strings.TrimSpace(part)
		if i := strings.IndexByte(media, ';'); i >= 0 {
			media = media[:i]
		}
		switch strings.TrimSpace(media) {
		case "text/markdown", "text/plain":
			return true
		}
	}
	return false
}
