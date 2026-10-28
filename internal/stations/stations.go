// Package stations resolves UK rail station names to CRS codes.
//
// The index is built from an embedded seed list of major stations. It is not
// exhaustive: any well-formed three-letter code is accepted by [Index.Resolve]
// whether or not it appears in the list, and the upstream service decides
// whether it exists.
package stations

import (
	"bytes"
	_ "embed"
	"encoding/csv"
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"
	"unicode"
)

//go:embed stations.csv
var seedCSV []byte

var (
	// ErrNotFound is returned when no station matches a query.
	ErrNotFound = errors.New("no matching station")

	// ErrAmbiguous is returned when a query matches several stations equally
	// well. Use errors.As with *AmbiguousError to retrieve the candidates.
	ErrAmbiguous = errors.New("ambiguous station")

	// ErrInvalidData is returned by NewIndex when the station list is malformed.
	ErrInvalidData = errors.New("invalid station data")
)

// Station is a rail station and its CRS code.
type Station struct {
	// CRS is the three-letter Computer Reservation System code, upper case.
	CRS string `json:"crs"`
	// Name is the display name.
	Name string `json:"name"`
}

// AmbiguousError reports the stations that tied for a query.
type AmbiguousError struct {
	Query      string
	Candidates []Station
}

// Error implements error.
func (e *AmbiguousError) Error() string {
	names := make([]string, len(e.Candidates))
	for i, s := range e.Candidates {
		names[i] = fmt.Sprintf("%s (%s)", s.Name, s.CRS)
	}
	return fmt.Sprintf("%v: %q matches %s", ErrAmbiguous, e.Query, strings.Join(names, ", "))
}

// Unwrap lets errors.Is match ErrAmbiguous.
func (e *AmbiguousError) Unwrap() error { return ErrAmbiguous }

type entry struct {
	Station
	norm   string
	tokens []string
}

// Index is an immutable, concurrency-safe station lookup.
type Index struct {
	entries []entry
	byCRS   map[string]Station
}

// New builds an Index from the embedded seed list.
func New() (*Index, error) {
	return NewIndex(seedCSV)
}

// NewIndex builds an Index from CSV data with a "crs,name" header row.
func NewIndex(data []byte) (*Index, error) {
	rows, err := csv.NewReader(bytes.NewReader(data)).ReadAll()
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrInvalidData, err)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("%w: no stations", ErrInvalidData)
	}

	idx := &Index{byCRS: make(map[string]Station, len(rows))}
	for _, row := range rows[1:] {
		if len(row) != 2 || !validCRS(row[0]) || strings.TrimSpace(row[1]) == "" {
			return nil, fmt.Errorf("%w: bad row %q", ErrInvalidData, row)
		}
		s := Station{CRS: strings.ToUpper(row[0]), Name: strings.TrimSpace(row[1])}
		if _, dup := idx.byCRS[s.CRS]; dup {
			return nil, fmt.Errorf("%w: duplicate code %s", ErrInvalidData, s.CRS)
		}
		idx.byCRS[s.CRS] = s
		norm := normalize(s.Name)
		idx.entries = append(idx.entries, entry{Station: s, norm: norm, tokens: strings.Fields(norm)})
	}
	return idx, nil
}

// Len reports how many stations are indexed.
func (i *Index) Len() int { return len(i.entries) }

// Lookup returns the station with the given CRS code.
func (i *Index) Lookup(crs string) (Station, bool) {
	s, ok := i.byCRS[strings.ToUpper(strings.TrimSpace(crs))]
	return s, ok
}

// Search returns up to limit stations matching query, best match first.
// A limit of zero or less returns every match.
func (i *Index) Search(query string, limit int) []Station {
	matches := i.search(query)
	if limit > 0 && len(matches) > limit {
		matches = matches[:limit]
	}
	out := make([]Station, len(matches))
	for n, m := range matches {
		out[n] = m.Station
	}
	return out
}

// Resolve maps user input to a single station.
//
// Input that is a well-formed CRS code resolves to that code even when it is
// not in the index, in which case Station.Name is empty. Anything else is
// matched by name: the best match wins, and a tie for first place returns an
// *AmbiguousError. No match returns ErrNotFound.
func (i *Index) Resolve(input string) (Station, error) {
	input = strings.TrimSpace(input)
	if validCRS(input) {
		if s, ok := i.Lookup(input); ok {
			return s, nil
		}
		// The seed list is not exhaustive; the upstream service decides
		// whether an unlisted code exists.
		return Station{CRS: strings.ToUpper(input)}, nil
	}

	matches := i.search(input)
	switch {
	case len(matches) == 0:
		return Station{}, fmt.Errorf("%w: %q", ErrNotFound, input)
	case len(matches) == 1 || matches[0].score > matches[1].score:
		return matches[0].Station, nil
	}

	var tied []Station
	for _, m := range matches {
		if m.score != matches[0].score || len(tied) == 5 {
			break
		}
		tied = append(tied, m.Station)
	}
	return Station{}, &AmbiguousError{Query: input, Candidates: tied}
}

type match struct {
	Station
	score int
}

const (
	scoreExact  = 100
	scorePrefix = 80
	scoreTokens = 60
)

func (i *Index) search(query string) []match {
	q := normalize(query)
	qTokens := strings.Fields(q)
	if len(qTokens) == 0 {
		return nil
	}

	var matches []match
	for _, e := range i.entries {
		if score := scoreEntry(e, q, qTokens); score > 0 {
			matches = append(matches, match{Station: e.Station, score: score})
		}
	}
	sort.SliceStable(matches, func(a, b int) bool {
		if matches[a].score != matches[b].score {
			return matches[a].score > matches[b].score
		}
		if la, lb := len(matches[a].Name), len(matches[b].Name); la != lb {
			return la < lb
		}
		return matches[a].Name < matches[b].Name
	})
	return matches
}

// scoreEntry ranks an entry: an exact name beats a name prefix, which beats
// every query word being the start of some name word.
func scoreEntry(e entry, q string, qTokens []string) int {
	switch {
	case e.norm == q:
		return scoreExact
	case strings.HasPrefix(e.norm, q):
		return scorePrefix
	}
	for _, qt := range qTokens {
		if !slices.ContainsFunc(e.tokens, func(t string) bool { return strings.HasPrefix(t, qt) }) {
			return 0
		}
	}
	return scoreTokens
}

func validCRS(s string) bool {
	if len(s) != 3 {
		return false
	}
	for _, r := range s {
		if r > unicode.MaxASCII || !unicode.IsLetter(r) {
			return false
		}
	}
	return true
}

// normalize lower-cases s, drops apostrophes, turns other punctuation into
// spaces, and collapses whitespace, so "King's Cross" and "kings cross" agree.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r == '\'' || r == '’':
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}
