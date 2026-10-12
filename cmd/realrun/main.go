// Command realrun runs the engine in memory over source CSVs prepared from an
// outside dataset and, when a truth file is given, scores the matches against
// the links the dataset itself carries. It needs no database.
//
// The truth file has the header source,row,label,type. row is the 1-based data
// row in that source's CSV. Rows that share a label belong in one match; an
// empty label means the row should stay unmatched. type, when set, is the
// discrepancy type the dataset says the row should get.
package main

import (
	"encoding/csv"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"time"

	"reconpilot/internal/domain"
	"reconpilot/internal/engine"
	"reconpilot/internal/ingestion"
)

type rowKey struct {
	source domain.SourceType
	row    int
}

type sourceStats struct {
	Rows     int            `json:"rows"`
	Accepted int            `json:"accepted"`
	Rejected int            `json:"rejected"`
	Reasons  map[string]int `json:"rejectReasons,omitempty"`
}

type truthScore struct {
	Labelled       int                       `json:"labelledRows"`
	Unlabelled     int                       `json:"unlabelledRows"`
	IntendedGroups int                       `json:"intendedGroups"`
	WholeGroups    int                       `json:"intendedGroupsMatchedWhole"`
	MissedGroups   int                       `json:"intendedGroupsMissed"`
	Matches        int                       `json:"matches"`
	CorrectMatches int                       `json:"correctMatches"`
	FalseMatches   int                       `json:"falseMatches"`
	FalseByKind    map[string]int            `json:"falseMatchesByKind"`
	TypeConfusion  map[string]map[string]int `json:"typeConfusion"`
	FalseExamples  [][]string                `json:"falseMatchExamples,omitempty"`
	MissedByReason map[string]int            `json:"missedGroupsByTruthType,omitempty"`
}

type summary struct {
	Sources       map[string]sourceStats `json:"sources"`
	Transactions  int                    `json:"transactions"`
	Elapsed       string                 `json:"elapsed"`
	MatchesByKind map[string]int         `json:"matchesByKind"`
	Discrepancies map[string]int         `json:"discrepanciesByType"`
	DeltaByType   map[string]int64       `json:"deltaKurusByType"`
	Truth         *truthScore            `json:"truth,omitempty"`
}

func main() {
	psp := flag.String("psp", "", "PSP CSV")
	bank := flag.String("bank", "", "bank CSV")
	mkt := flag.String("marketplace", "", "marketplace CSV")
	truthPath := flag.String("truth", "", "truth CSV (optional)")
	outDir := flag.String("out", "realrun-out", "output directory")
	flag.Parse()

	if err := run(map[domain.SourceType]string{
		domain.SourcePSP: *psp, domain.SourceBank: *bank, domain.SourceMarketplace: *mkt,
	}, *truthPath, *outDir); err != nil {
		fmt.Fprintln(os.Stderr, "realrun:", err)
		os.Exit(1)
	}
}

func run(files map[domain.SourceType]string, truthPath, outDir string) error {
	var all []domain.Transaction
	keyOf := map[int64]rowKey{}
	idOf := map[rowKey]int64{}
	sum := summary{Sources: map[string]sourceStats{}, MatchesByKind: map[string]int{},
		Discrepancies: map[string]int{}, DeltaByType: map[string]int64{}}

	// IDs follow source then file order so the truth file can name rows.
	for _, src := range []domain.SourceType{domain.SourcePSP, domain.SourceBank, domain.SourceMarketplace} {
		path := files[src]
		if path == "" {
			continue
		}
		txs, rejected, err := parseFile(src, path)
		if err != nil {
			return fmt.Errorf("%s: %w", src, err)
		}
		st := sourceStats{Rows: len(txs) + len(rejected), Accepted: len(txs), Rejected: len(rejected),
			Reasons: map[string]int{}}
		skip := map[int]bool{}
		for _, r := range rejected {
			skip[r.RowNo] = true
			st.Reasons[reasonClass(r.Reason)]++
		}
		rowNo := 1 // the header is row 1 in the adapter's numbering
		for i := range txs {
			rowNo++
			for skip[rowNo] {
				rowNo++
			}
			txs[i].ID = int64(len(all) + 1)
			k := rowKey{src, rowNo - 1}
			keyOf[txs[i].ID] = k
			idOf[k] = txs[i].ID
			all = append(all, txs[i])
		}
		sum.Sources[string(src)] = st
	}
	sum.Transactions = len(all)

	start := time.Now()
	out, err := engine.Run(all)
	sum.Elapsed = time.Since(start).String()
	if err != nil {
		return fmt.Errorf("engine: %w", err)
	}
	for _, m := range out.Matches {
		sum.MatchesByKind[m.Kind]++
	}
	typeOf := map[int64]string{}
	for _, d := range out.Discrepancies {
		sum.Discrepancies[d.Type]++
		sum.DeltaByType[d.Type] += d.DeltaKurus
		if d.TxID != nil {
			typeOf[*d.TxID] = d.Type
		}
	}

	byID := make(map[int64]domain.Transaction, len(all))
	for _, t := range all {
		byID[t.ID] = t
	}
	if truthPath != "" {
		label, wantType, err := readTruth(truthPath, idOf)
		if err != nil {
			return err
		}
		sum.Truth = score(all, out, label, wantType, typeOf, byID)
	}

	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	if err := writeMatches(filepath.Join(outDir, "matches.csv"), out, byID, keyOf); err != nil {
		return err
	}
	if err := writeDiscrepancies(filepath.Join(outDir, "discrepancies.csv"), out, byID, keyOf); err != nil {
		return err
	}
	b, err := json.MarshalIndent(sum, "", "  ")
	if err != nil {
		return err
	}
	fmt.Println(string(b))
	return os.WriteFile(filepath.Join(outDir, "summary.json"), b, 0o644)
}

func parseFile(src domain.SourceType, path string) ([]domain.Transaction, []ingestion.RejectedRow, error) {
	a, err := ingestion.AdapterFor(src)
	if err != nil {
		return nil, nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	return a.Parse(f)
}

// reasonClass cuts a reject reason at its first quote or colon, where the
// row-specific values begin, so the summary counts kinds of rejection.
func reasonClass(reason string) string {
	for i, r := range reason {
		if r == '"' || r == ':' {
			return reason[:i]
		}
	}
	return reason
}

func readTruth(path string, idOf map[rowKey]int64) (map[int64]string, map[int64]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, nil, err
	}
	defer f.Close()
	cr := csv.NewReader(f)
	header, err := cr.Read()
	if err != nil {
		return nil, nil, fmt.Errorf("truth header: %w", err)
	}
	if len(header) != 4 || header[0] != "source" || header[1] != "row" || header[2] != "label" || header[3] != "type" {
		return nil, nil, fmt.Errorf("truth header %v, want source,row,label,type", header)
	}
	label := map[int64]string{}
	wantType := map[int64]string{}
	for {
		rec, err := cr.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, nil, err
		}
		row, err := strconv.Atoi(rec[1])
		if err != nil {
			return nil, nil, fmt.Errorf("truth row %q: %w", rec[1], err)
		}
		id, ok := idOf[rowKey{domain.SourceType(rec[0]), row}]
		if !ok {
			continue // the adapter rejected this row
		}
		if rec[2] != "" {
			label[id] = rec[2]
		}
		if rec[3] != "" {
			wantType[id] = rec[3]
		}
	}
	return label, wantType, nil
}

func score(all []domain.Transaction, out engine.Output, label, wantType, typeOf map[int64]string,
	byID map[int64]domain.Transaction) *truthScore {
	s := &truthScore{FalseByKind: map[string]int{}, TypeConfusion: map[string]map[string]int{},
		MissedByReason: map[string]int{}}
	members := map[string][]int64{}
	for _, t := range all {
		if l, ok := label[t.ID]; ok {
			s.Labelled++
			members[l] = append(members[l], t.ID)
		} else {
			s.Unlabelled++
		}
	}
	// A label held by a single surviving row cannot be matched; it is not an
	// intended group.
	for _, ids := range members {
		if len(ids) > 1 {
			s.IntendedGroups++
		}
	}

	s.Matches = len(out.Matches)
	matchedIn := map[int64]int{}
	for mi, m := range out.Matches {
		l, ok := label[m.TxIDs[0]]
		clean := ok
		for _, id := range m.TxIDs {
			matchedIn[id] = mi
			if l2, ok2 := label[id]; !ok2 || l2 != l {
				clean = false
			}
		}
		if !clean {
			s.FalseMatches++
			s.FalseByKind[m.Kind]++
			if len(s.FalseExamples) < 20 {
				var ex []string
				ex = append(ex, m.Kind)
				for _, id := range m.TxIDs {
					t := byID[id]
					ex = append(ex, fmt.Sprintf("%s|%s|%s|%d|%s|label=%s", t.Source, t.ExternalRef,
						t.CounterpartyRef, t.AmountKurus, t.OccurredAt.Format("2006-01-02"), label[id]))
				}
				s.FalseExamples = append(s.FalseExamples, ex)
			}
			continue
		}
		s.CorrectMatches++
	}
	for _, ids := range members {
		if len(ids) < 2 {
			continue
		}
		mi, ok := matchedIn[ids[0]]
		whole := ok && len(out.Matches[mi].TxIDs) == len(ids)
		for _, id := range ids[1:] {
			if mj, ok := matchedIn[id]; !ok || mj != mi {
				whole = false
			}
		}
		if whole {
			s.WholeGroups++
			continue
		}
		s.MissedGroups++
		reason := ""
		for _, id := range ids {
			if wantType[id] != "" {
				reason = wantType[id]
			}
		}
		if reason == "" {
			reason = "(none)"
		}
		s.MissedByReason[reason]++
	}

	for _, t := range all {
		want := wantType[t.ID]
		if want == "" {
			want = "(none)"
		}
		got, ok := typeOf[t.ID]
		if !ok {
			if _, matched := matchedIn[t.ID]; matched {
				got = "(matched)"
			} else {
				got = "(group-level only)"
			}
		}
		if s.TypeConfusion[want] == nil {
			s.TypeConfusion[want] = map[string]int{}
		}
		s.TypeConfusion[want][got]++
	}
	return s
}

func writeMatches(path string, out engine.Output, byID map[int64]domain.Transaction, keyOf map[int64]rowKey) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"match", "kind", "score_bp", "source", "row", "ref", "counterparty", "amount_kurus", "date", "direction"})
	for mi, m := range out.Matches {
		for _, id := range m.TxIDs {
			t := byID[id]
			w.Write([]string{strconv.Itoa(mi), m.Kind, strconv.FormatInt(m.ScoreBP, 10), string(t.Source),
				strconv.Itoa(keyOf[id].row), t.ExternalRef, t.CounterpartyRef,
				strconv.FormatInt(t.AmountKurus, 10), t.OccurredAt.Format("2006-01-02"), string(t.Direction)})
		}
	}
	w.Flush()
	return w.Error()
}

func writeDiscrepancies(path string, out engine.Output, byID map[int64]domain.Transaction, keyOf map[int64]rowKey) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := csv.NewWriter(f)
	w.Write([]string{"type", "delta_kurus", "match", "source", "row", "ref", "counterparty", "amount_kurus", "date", "direction"})
	discs := append(out.Discrepancies[:0:0], out.Discrepancies...)
	sort.SliceStable(discs, func(i, j int) bool { return discs[i].Type < discs[j].Type })
	for _, d := range discs {
		if d.TxID == nil {
			w.Write([]string{d.Type, strconv.FormatInt(d.DeltaKurus, 10), strconv.Itoa(*d.MatchIdx), "", "", "", "", "", "", ""})
			continue
		}
		t := byID[*d.TxID]
		w.Write([]string{d.Type, strconv.FormatInt(d.DeltaKurus, 10), "", string(t.Source),
			strconv.Itoa(keyOf[*d.TxID].row), t.ExternalRef, t.CounterpartyRef,
			strconv.FormatInt(t.AmountKurus, 10), t.OccurredAt.Format("2006-01-02"), string(t.Direction)})
	}
	w.Flush()
	return w.Error()
}
