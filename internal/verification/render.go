package verification

import (
	"bytes"
	"embed"
	"html/template"
)

//go:embed panel.html.tmpl
var tmplFS embed.FS

// page flattens a Result for the template. Templates cannot call a
// multi-return method, so the derived figures are resolved here.
type page struct {
	Passed         bool
	TypesOK        bool
	DetectedTypes  int
	InjectedTypes  int
	FalseMatches   int
	MissedIntended int
	Rows           []TypeRow
	CleanPairs     int
	CleanGroups    int
	Matches        int
	N              int
	Seed           int64
	Elapsed        string
}

// RenderHTML renders a scored run as a self-contained HTML page. It embeds
// its own styling and pulls in no external asset, so it works from a file://
// URL and from a scratch container alike.
func RenderHTML(r Result) ([]byte, error) {
	detected, injected := r.DetectedTypes()
	p := page{
		Passed:         r.Passed(),
		TypesOK:        detected == injected,
		DetectedTypes:  detected,
		InjectedTypes:  injected,
		FalseMatches:   r.FalseMatches,
		MissedIntended: r.MissedIntended,
		Rows:           r.Rows,
		CleanPairs:     r.CleanPairs,
		CleanGroups:    r.CleanGroups,
		Matches:        r.Matches,
		N:              r.N,
		Seed:           r.Seed,
		Elapsed:        r.Elapsed.String(),
	}
	tmpl, err := template.ParseFS(tmplFS, "panel.html.tmpl")
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, p); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
