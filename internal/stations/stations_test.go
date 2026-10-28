package stations_test

import (
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/elliottsam/rail-timetable-mcp-uk/internal/stations"
)

func newIndex(t *testing.T) *stations.Index {
	t.Helper()
	idx, err := stations.New()
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return idx
}

func TestEmbeddedListIsValid(t *testing.T) {
	if n := newIndex(t).Len(); n < 100 {
		t.Fatalf("Len = %d, want a seed list of at least 100", n)
	}
}

func TestResolve(t *testing.T) {
	idx := newIndex(t)

	tests := []struct {
		name    string
		input   string
		wantCRS string
		wantErr error
	}{
		{name: "listed code", input: "KGX", wantCRS: "KGX"},
		{name: "lower case code", input: "kgx", wantCRS: "KGX"},
		{name: "padded code", input: "  pad ", wantCRS: "PAD"},
		{name: "unlisted code passes through", input: "zzz", wantCRS: "ZZZ"},
		{name: "exact name", input: "Edinburgh Waverley", wantCRS: "EDB"},
		{name: "apostrophe-insensitive", input: "kings cross", wantCRS: "KGX"},
		{name: "name prefix", input: "birmingham new", wantCRS: "BHM"},
		{name: "unique word", input: "waverley", wantCRS: "EDB"},
		{name: "punctuation", input: "Stoke-on-Trent", wantCRS: "SOT"},
		{name: "no match", input: "atlantis", wantErr: stations.ErrNotFound},
		{name: "empty", input: "", wantErr: stations.ErrNotFound},
		{name: "only punctuation", input: "!!!", wantErr: stations.ErrNotFound},
		{name: "ambiguous", input: "london", wantErr: stations.ErrAmbiguous},
		{name: "digit in code is a name", input: "k1x", wantErr: stations.ErrNotFound},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := idx.Resolve(tt.input)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Resolve(%q) error = %v, want %v", tt.input, err, tt.wantErr)
			}
			if got.CRS != tt.wantCRS {
				t.Errorf("Resolve(%q) = %q, want %q", tt.input, got.CRS, tt.wantCRS)
			}
		})
	}
}

func TestResolveAmbiguousListsCandidates(t *testing.T) {
	_, err := newIndex(t).Resolve("london")

	var amb *stations.AmbiguousError
	if !errors.As(err, &amb) {
		t.Fatalf("error = %v, want *AmbiguousError", err)
	}
	if n := len(amb.Candidates); n < 2 || n > 5 {
		t.Errorf("candidates = %d, want 2..5", n)
	}
	if !strings.Contains(err.Error(), amb.Candidates[0].CRS) {
		t.Errorf("message %q omits candidate code", err)
	}
}

func TestSearchOrdersBestFirst(t *testing.T) {
	got := newIndex(t).Search("reading", 0)
	if len(got) == 0 || got[0].CRS != "RDG" {
		t.Fatalf("Search(reading) = %v, want RDG first", got)
	}
}

func TestSearchLimit(t *testing.T) {
	idx := newIndex(t)
	if got := idx.Search("london", 3); len(got) != 3 {
		t.Errorf("limit 3 returned %d", len(got))
	}
	if got := idx.Search("london", 0); len(got) <= 3 {
		t.Errorf("no limit returned %d, want all matches", len(got))
	}
}

func TestLookup(t *testing.T) {
	idx := newIndex(t)
	if s, ok := idx.Lookup(" yrk "); !ok || s.Name != "York" {
		t.Errorf("Lookup(yrk) = %v, %v", s, ok)
	}
	if _, ok := idx.Lookup("QQQ"); ok {
		t.Error("Lookup(QQQ) found a station")
	}
}

func TestNewIndexRejectsBadData(t *testing.T) {
	tests := map[string]string{
		"empty":          "",
		"header only":    "crs,name\n",
		"bad code":       "crs,name\nAB,Nowhere\n",
		"non-letter":     "crs,name\nA1B,Nowhere\n",
		"blank name":     "crs,name\nABC, \n",
		"duplicate code": "crs,name\nABC,One\nabc,Two\n",
		"extra column":   "crs,name\nABC,One,Two\n",
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := stations.NewIndex([]byte(data)); !errors.Is(err, stations.ErrInvalidData) {
				t.Fatalf("error = %v, want ErrInvalidData", err)
			}
		})
	}
}

func FuzzResolve(f *testing.F) {
	idx, err := stations.New()
	if err != nil {
		f.Fatal(err)
	}
	for _, seed := range []string{"KGX", "kings cross", "", "  ", "london", "’", "a b c", "ÀÉÎ"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, input string) {
		st, err := idx.Resolve(input)
		if err != nil {
			if !errors.Is(err, stations.ErrNotFound) && !errors.Is(err, stations.ErrAmbiguous) {
				t.Fatalf("unexpected error type: %v", err)
			}
			return
		}
		if len(st.CRS) != 3 || !utf8.ValidString(st.CRS) || st.CRS != strings.ToUpper(st.CRS) {
			t.Fatalf("Resolve(%q) produced bad CRS %q", input, st.CRS)
		}
	})
}
