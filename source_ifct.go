package main

import (
	"context"
	_ "embed"
	"encoding/csv"
	"strconv"
	"strings"
)

// IFCT is the embedded, offline-first Indian food table.
//
// The CSV at `data/ifct_starter.csv` is a hand-curated subset of the
// Indian Food Composition Tables 2017 (NIN Hyderabad, A. Longvah et al.)
// plus some popular branded items pulled from manufacturer labels. The
// numbers are per-100g and approximate — within ~5% of what you'd see
// printed on the side of the package. Tune individual rows as you find
// better data; the CSV is plain text and easy to edit.
//
// Why embed instead of load-from-disk:
//   The binary is fully self-contained. Drop `trackbite` on a fresh
//   MacBook and it works without you remembering to copy the data file.
//
// Why no API call:
//   IFCT 2017 has no public API. The original publication is a 500-page
//   PDF + an Excel supplement; the CSV here is a digitised slice.

//go:embed data/ifct_starter.csv
var ifctCSVRaw []byte

type IFCT struct {
	rows []NutritionInfo
}

func NewIFCT() (*IFCT, error) {
	r := csv.NewReader(strings.NewReader(string(ifctCSVRaw)))
	r.FieldsPerRecord = -1 // tolerate trailing-comma quirks
	records, err := r.ReadAll()
	if err != nil {
		return nil, err
	}
	out := make([]NutritionInfo, 0, len(records))
	for i, rec := range records {
		if i == 0 || len(rec) < 5 {
			continue // header or malformed
		}
		kcal := parseFloat(rec[1])
		if kcal == 0 {
			continue
		}
		source := "IFCT-2017 (local)"
		if len(rec) >= 6 && rec[5] != "" {
			source = "IFCT-2017 / " + rec[5]
		}
		out = append(out, NutritionInfo{
			Name:           strings.TrimSpace(rec[0]),
			Source:         source,
			KcalPer100g:    kcal,
			ProteinPer100g: parseFloat(rec[2]),
			CarbsPer100g:   parseFloat(rec[3]),
			FatPer100g:     parseFloat(rec[4]),
		})
	}
	return &IFCT{rows: out}, nil
}

func (i *IFCT) Name() string { return "IFCT (local)" }

// IFCT has no barcode information — it's a generic food table.
func (i *IFCT) LookupBarcode(ctx context.Context, code string) (*NutritionInfo, error) {
	return nil, ErrNotFound
}

// LookupName does a tiny fuzzy match: lowercase, strip punctuation, and
// pick the row with the longest common-substring overlap with the
// query. This is intentionally dumb — anything fancier would need a
// real search index, and for ~100 rows the naive scan is microseconds.
func (i *IFCT) LookupName(ctx context.Context, query string) (*NutritionInfo, error) {
	q := normalize(query)
	if q == "" {
		return nil, ErrNotFound
	}
	var best *NutritionInfo
	bestScore := 0
	for idx := range i.rows {
		r := &i.rows[idx]
		score := overlap(normalize(r.Name), q)
		if score > bestScore {
			bestScore = score
			best = r
		}
	}
	if best == nil || bestScore < 3 {
		// fewer than 3 matching characters means it's basically random
		return nil, ErrNotFound
	}
	// Return a copy so callers can't mutate our table.
	out := *best
	return &out, nil
}

func parseFloat(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return v
}

func normalize(s string) string {
	s = strings.ToLower(s)
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-':
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// overlap counts the longest contiguous substring of `query` that
// appears anywhere in `name`. Cheap, no dependencies, and surprisingly
// effective for short food names.
func overlap(name, query string) int {
	best := 0
	for i := 0; i < len(query); i++ {
		for j := i + 1; j <= len(query); j++ {
			sub := query[i:j]
			if len(sub) > best && strings.Contains(name, sub) {
				best = len(sub)
			}
		}
	}
	return best
}
