// Package reporting renders an engine run as a self-contained HTML page and
// a machine-readable CSV. Money is formatted from kurus at the last moment;
// nothing upstream ever leaves int64.
package reporting

import (
	"bytes"
	"embed"
	"fmt"
	"html/template"
	"time"

	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
)

//go:embed report.html.tmpl
var tmplFS embed.FS

type matchRow struct {
	Kind  string
	Count int
}
type discRow struct {
	Type     string
	Count    int
	TotalTRY string
}
type ageRow struct {
	Bucket string
	Count  int
}
type page struct {
	TotalTx      int
	MatchRatePct string
	MatchRows    []matchRow
	DiscRows     []discRow
	AgeRows      []ageRow
}

// FormatTRY renders kurus as a TRY string: 12345 → "123,45".
func FormatTRY(kurus int64) string {
	sign := ""
	if kurus < 0 {
		sign, kurus = "-", -kurus
	}
	return fmt.Sprintf("%s%d,%02d", sign, kurus/100, kurus%100)
}

func Render(out engine.Output, all []domain.Transaction) ([]byte, []byte, error) {
	byID := map[int64]domain.Transaction{}
	var newest time.Time
	for _, t := range all {
		byID[t.ID] = t
		if t.OccurredAt.After(newest) {
			newest = t.OccurredAt
		}
	}
	matched := 0
	kinds := map[string]int{}
	for _, m := range out.Matches {
		kinds[m.Kind]++
		matched += len(m.TxIDs)
	}
	p := page{TotalTx: out.TotalTx}
	if out.TotalTx > 0 {
		p.MatchRatePct = fmt.Sprintf("%d", matched*100/out.TotalTx)
	}
	for _, k := range []string{"exact", "tolerant", "group"} {
		p.MatchRows = append(p.MatchRows, matchRow{k, kinds[k]})
	}
	discCount := map[string]int{}
	discTotal := map[string]int64{}
	ages := map[string]int{"0-7d": 0, "8-30d": 0, "30d+": 0}
	var csvBuf bytes.Buffer
	csvBuf.WriteString("type,tx_id,delta_kurus\n")
	for _, d := range out.Discrepancies {
		discCount[d.Type]++
		discTotal[d.Type] += d.DeltaKurus
		txID := int64(0)
		if d.TxID != nil {
			txID = *d.TxID
			ageDays := int(newest.Sub(byID[txID].OccurredAt).Hours() / 24)
			switch {
			case ageDays <= 7:
				ages["0-7d"]++
			case ageDays <= 30:
				ages["8-30d"]++
			default:
				ages["30d+"]++
			}
		}
		fmt.Fprintf(&csvBuf, "%s,%d,%d\n", d.Type, txID, d.DeltaKurus)
	}
	for _, typ := range []string{"commission", "refund", "partial", "timing", "duplicate", "missing", "unknown"} {
		p.DiscRows = append(p.DiscRows, discRow{typ, discCount[typ], FormatTRY(discTotal[typ])})
	}
	for _, b := range []string{"0-7d", "8-30d", "30d+"} {
		p.AgeRows = append(p.AgeRows, ageRow{b, ages[b]})
	}
	tmpl, err := template.ParseFS(tmplFS, "report.html.tmpl")
	if err != nil {
		return nil, nil, err
	}
	var htmlBuf bytes.Buffer
	if err := tmpl.Execute(&htmlBuf, p); err != nil {
		return nil, nil, err
	}
	return htmlBuf.Bytes(), csvBuf.Bytes(), nil
}
