package ratelimit

import (
	"bytes"
	"math"
	"regexp"
	"strings"
	"testing"
	"time"
)

func TestTypesCompile(t *testing.T) {
	r := Resource{Name: "core", Limit: 5000, Remaining: 4500, Reset: 1}
	l := Limit{Name: r.Name, Remaining: r.Remaining, Limit: r.Limit, Reset: r.Reset, Pct: 90}
	if l.Name != "core" || l.Pct != 90 {
		t.Fatalf("unexpected: %+v", l)
	}
}

func TestFormatReset(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	cases := []struct {
		name  string
		reset int64
		want  string
	}{
		{"now exact", 1_000_000, "now"},
		{"past", 999_000, "now"},
		{"seconds only", 1_000_045, "45s"},
		{"single digit seconds", 1_000_005, "5s"},
		{"minutes and seconds", 1_000_950, "15m 50s"},
		{"zero-padded seconds", 1_000_905, "15m 05s"},
		{"one minute exact", 1_000_060, "1m 00s"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := FormatReset(c.reset, now); got != c.want {
				t.Errorf("FormatReset(%d) = %q, want %q", c.reset, got, c.want)
			}
		})
	}
}

func TestCenter(t *testing.T) {
	cases := []struct {
		s     string
		width int
		want  string
	}{
		{"ab", 5, " ab  "},                        // marg 3 odd -> :^ puts extra space on the right
		{"Resource", 21, "      Resource       "}, // 6 left, 7 right; locks :^ vs str.center
		{"a", 4, " a  "},                          // marg 3, width 4 -> 1 left, 2 right
		{"Resource", 20, "      Resource      "},
		{"Remaining", 19, "     Remaining     "},
		{"Meter", 20, "       Meter        "},
		{"abc", 3, "abc"},       // n == width
		{"abcdef", 4, "abcdef"}, // n > width: returned unchanged
	}
	for _, c := range cases {
		if got := center(c.s, c.width); got != c.want {
			t.Errorf("center(%q,%d) = %q, want %q", c.s, c.width, got, c.want)
		}
	}
}

func countRune(s string, r rune) int {
	n := 0
	for _, c := range s {
		if c == r {
			n++
		}
	}
	return n
}

func TestBar(t *testing.T) {
	// limit == 0 -> 20 spaces, no color.
	if got := Bar(0, 0, true); got != strings.Repeat(" ", 20) {
		t.Errorf("Bar(0,0) = %q, want 20 spaces", got)
	}
	// color off, full bar.
	if got := Bar(5000, 5000, false); got != strings.Repeat("█", 20) {
		t.Errorf("Bar full = %q", got)
	}
	// color off, empty bar.
	if got := Bar(0, 5000, false); got != strings.Repeat("░", 20) {
		t.Errorf("Bar empty = %q", got)
	}
	// fill counts: 0.7034 -> 14 filled, 6 empty.
	b := Bar(3517, 5000, false)
	if countRune(b, '█') != 14 || countRune(b, '░') != 6 {
		t.Errorf("Bar(3517,5000) fills = %d/%d, want 14/6", countRune(b, '█'), countRune(b, '░'))
	}
	// color thresholds (fraction): >0.5 green, >0.2 yellow, else red.
	if g := Bar(3517, 5000, true); !strings.HasPrefix(g, "\033[32m") || !strings.HasSuffix(g, "\033[0m") {
		t.Errorf("Bar 0.70 should be green-wrapped: %q", g)
	}
	if y := Bar(1500, 5000, true); !strings.HasPrefix(y, "\033[33m") {
		t.Errorf("Bar 0.30 should be yellow: %q", y)
	}
	if r := Bar(500, 5000, true); !strings.HasPrefix(r, "\033[31m") {
		t.Errorf("Bar 0.10 should be red: %q", r)
	}
}

func TestParseResourcesPreservesOrder(t *testing.T) {
	data := []byte(`{"resources":{"search":{"limit":30,"remaining":30,"reset":30},` +
		`"core":{"limit":5000,"remaining":4500,"reset":10},` +
		`"graphql":{"limit":5000,"remaining":3517,"reset":20}}}`)
	got, err := ParseResources(data)
	if err != nil {
		t.Fatal(err)
	}
	wantNames := []string{"search", "core", "graphql"}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	for i, n := range wantNames {
		if got[i].Name != n {
			t.Errorf("order[%d] = %q, want %q", i, got[i].Name, n)
		}
	}
	if got[1].Limit != 5000 || got[1].Remaining != 4500 || got[1].Reset != 10 {
		t.Errorf("core fields wrong: %+v", got[1])
	}
}

func TestParseResourcesMissing(t *testing.T) {
	got, err := ParseResources([]byte(`{}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Errorf("want empty, got %+v", got)
	}
}

func TestProcessFilterAndSort(t *testing.T) {
	res := []Resource{
		{Name: "core", Limit: 5000, Remaining: 4500, Reset: 1},      // 90%
		{Name: "search", Limit: 30, Remaining: 30, Reset: 1},        // filtered (<=100)
		{Name: "audit_log", Limit: 5000, Remaining: 5000, Reset: 1}, // filtered (hidden)
		{Name: "graphql", Limit: 5000, Remaining: 3517, Reset: 1},   // 70.34%
	}
	// Default: only core + graphql, ascending by pct -> graphql, core.
	def := Process(res, false)
	if len(def) != 2 || def[0].Name != "graphql" || def[1].Name != "core" {
		t.Fatalf("default order wrong: %+v", names(def))
	}
	if math.Abs(def[0].Pct-70.34) > 1e-9 {
		t.Errorf("graphql pct = %v, want 70.34", def[0].Pct)
	}
	// All: keep all 4; ties at 100%% (search then audit_log) keep input order.
	all := Process(res, true)
	wantAll := []string{"graphql", "core", "search", "audit_log"}
	if got := names(all); !equal(got, wantAll) {
		t.Errorf("all order = %v, want %v", got, wantAll)
	}
}

func TestProcessZeroLimit(t *testing.T) {
	res := []Resource{{Name: "zero", Limit: 0, Remaining: 0, Reset: 1}}
	got := Process(res, true)
	if len(got) != 1 || got[0].Pct != 0 {
		t.Fatalf("zero-limit handling wrong: %+v", got)
	}
}

func TestBelowThreshold(t *testing.T) {
	limits := []Limit{{Pct: 70}, {Pct: 15}}
	if !BelowThreshold(limits, 20) {
		t.Error("expected below threshold (15 < 20)")
	}
	if BelowThreshold(limits, 10) {
		t.Error("expected not below threshold")
	}
}

// helpers for assertions
func names(ls []Limit) []string {
	out := make([]string, len(ls))
	for i, l := range ls {
		out[i] = l.Name
	}
	return out
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestRenderJSON(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	limits := []Limit{
		{Name: "graphql", Remaining: 3517, Limit: 5000, Reset: 1_000_950, Pct: 70.34},
		{Name: "core", Remaining: 4500, Limit: 5000, Reset: 1_000_922, Pct: 90.0},
	}
	var buf bytes.Buffer
	if err := RenderJSON(&buf, limits, now); err != nil {
		t.Fatal(err)
	}
	want := `[
  {
    "resource": "graphql",
    "remaining": 3517,
    "limit": 5000,
    "reset_timestamp": 1000950,
    "reset_in": "15m 50s",
    "percent_remaining": 70.3
  },
  {
    "resource": "core",
    "remaining": 4500,
    "limit": 5000,
    "reset_timestamp": 1000922,
    "reset_in": "15m 22s",
    "percent_remaining": 90.0
  }
]
`
	if buf.String() != want {
		t.Errorf("RenderJSON mismatch:\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}

func TestRenderJSONEmpty(t *testing.T) {
	var buf bytes.Buffer
	if err := RenderJSON(&buf, nil, time.Unix(0, 0)); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "[]\n" {
		t.Errorf("empty RenderJSON = %q, want \"[]\\n\"", buf.String())
	}
}

var ansiRE = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(s string) string { return ansiRE.ReplaceAllString(s, "") }

func TestRenderTableEmpty(t *testing.T) {
	var buf bytes.Buffer
	RenderTable(&buf, nil, time.Unix(0, 0), false)
	if buf.String() != "No rate limits to display.\n" {
		t.Errorf("empty table = %q", buf.String())
	}
}

func TestRenderTableColorOff(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	limits := []Limit{
		{Name: "code_scanning_upload", Remaining: 4500, Limit: 5000, Reset: 1_000_922, Pct: 90.0},
	}
	var buf bytes.Buffer
	RenderTable(&buf, limits, now, false)
	out := buf.String()

	if !strings.HasPrefix(out, "\n") {
		t.Error("table should start with a blank line")
	}
	if !strings.HasSuffix(out, "\n\n") {
		t.Error("table should end with a blank line")
	}
	if strings.Contains(out, "\x1b[") {
		t.Error("color-off output must contain no ANSI codes")
	}
	sep := "  " + strings.Repeat("-", 20) + "   " + strings.Repeat("-", 19) +
		"   " + strings.Repeat("-", 20) + "   " + strings.Repeat("-", 10)
	if !strings.Contains(out, sep) {
		t.Errorf("missing separator line; got:\n%s", out)
	}
	row := stripANSI(out)
	for _, want := range []string{"code_scanning_upload", "4500/5000   90%", "15m 22s"} {
		if !strings.Contains(row, want) {
			t.Errorf("row missing %q in:\n%s", want, row)
		}
	}
	if countRune(out, '█') != 18 || countRune(out, '░') != 2 {
		t.Errorf("bar fills = %d/%d, want 18/2", countRune(out, '█'), countRune(out, '░'))
	}
}

func TestRenderTableColorOnLowPct(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	limits := []Limit{
		{Name: "core", Remaining: 500, Limit: 5000, Reset: 1_000_922, Pct: 10.0},
	}
	var buf bytes.Buffer
	RenderTable(&buf, limits, now, true)
	out := buf.String()
	if !strings.Contains(out, "\033[31m") { // red name (<20) and red bar (<=0.2)
		t.Errorf("expected red ANSI in:\n%s", out)
	}
	if !strings.Contains(out, "\033[0m") {
		t.Error("expected ANSI reset")
	}
}
