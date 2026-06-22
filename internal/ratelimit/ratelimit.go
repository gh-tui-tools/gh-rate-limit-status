// Package ratelimit contains the pure logic for gh-rate-limit-status:
// parsing the GET /rate_limit response, filtering and sorting it, and
// rendering it as a table or JSON. It performs no network or terminal I/O.
package ratelimit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Resource is one rate-limit category from the GET /rate_limit response.
type Resource struct {
	Name      string
	Limit     int
	Remaining int
	Reset     int64 // unix timestamp (seconds)
}

// Limit is a processed resource with its computed percent remaining.
type Limit struct {
	Name      string
	Remaining int
	Limit     int
	Reset     int64
	Pct       float64 // percent remaining, 0..100
}

// FormatReset renders the reset timestamp relative to now, e.g. "15m 30s",
// "45s", or "now" if the reset is at or before now.
func FormatReset(reset int64, now time.Time) string {
	delta := time.Unix(reset, 0).Sub(now)
	if delta <= 0 {
		return "now"
	}
	total := int(delta.Seconds())
	minutes := total / 60
	seconds := total % 60
	if minutes > 0 {
		return fmt.Sprintf("%dm %02ds", minutes, seconds)
	}
	return fmt.Sprintf("%ds", seconds)
}

// center reproduces Python's `:^` format-spec centering used for the table
// headers (f"{s:^width}"): for an odd margin the extra space goes on the
// right (left = margin/2). Strings at least as wide as width are returned
// unchanged.
func center(s string, width int) string {
	n := len(s)
	if n >= width {
		return s
	}
	marg := width - n
	left := marg / 2
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", marg-left)
}

const barWidth = 20

// Bar renders a 20-character meter for remaining/limit. When colorEnabled, it
// is wrapped in an ANSI color chosen by the remaining fraction: >0.5 green,
// >0.2 yellow, else red. A zero limit yields 20 blank spaces.
func Bar(remaining, limit int, colorEnabled bool) string {
	if limit == 0 {
		return strings.Repeat(" ", barWidth)
	}
	frac := float64(remaining) / float64(limit)
	filled := int(frac * barWidth)
	if filled < 0 {
		filled = 0
	}
	if filled > barWidth {
		filled = barWidth
	}
	bars := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
	if !colorEnabled {
		return bars
	}
	var color string
	switch {
	case frac > 0.5:
		color = "\033[32m"
	case frac > 0.2:
		color = "\033[33m"
	default:
		color = "\033[31m"
	}
	return color + bars + "\033[0m"
}

var hiddenResources = map[string]bool{
	"integration_manifest":        true,
	"actions_runner_registration": true,
	"scim":                        true,
	"audit_log":                   true,
}

// ParseResources decodes the GET /rate_limit body, returning the entries of the
// "resources" object in document order. A missing "resources" key yields an
// empty slice and no error.
func ParseResources(data []byte) ([]Resource, error) {
	var top struct {
		Resources json.RawMessage `json:"resources"`
	}
	if err := json.Unmarshal(data, &top); err != nil {
		return nil, err
	}
	if len(top.Resources) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(top.Resources))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if d, ok := tok.(json.Delim); !ok || d != '{' {
		return nil, fmt.Errorf("resources: expected object")
	}
	var out []Resource
	for dec.More() {
		keyTok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, _ := keyTok.(string)
		var info struct {
			Limit     int   `json:"limit"`
			Remaining int   `json:"remaining"`
			Reset     int64 `json:"reset"`
		}
		if err := dec.Decode(&info); err != nil {
			return nil, err
		}
		out = append(out, Resource{Name: name, Limit: info.Limit, Remaining: info.Remaining, Reset: info.Reset})
	}
	return out, nil
}

// Process filters resources (unless showAll) and returns them sorted ascending
// by percent remaining; equal percentages keep their input (API) order.
func Process(resources []Resource, showAll bool) []Limit {
	var limits []Limit
	for _, r := range resources {
		if showAll || (r.Limit > 100 && !hiddenResources[r.Name]) {
			pct := 0.0
			if r.Limit > 0 {
				pct = float64(r.Remaining) / float64(r.Limit) * 100
			}
			limits = append(limits, Limit{
				Name: r.Name, Remaining: r.Remaining, Limit: r.Limit,
				Reset: r.Reset, Pct: pct,
			})
		}
	}
	sort.SliceStable(limits, func(i, j int) bool { return limits[i].Pct < limits[j].Pct })
	return limits
}

// BelowThreshold reports whether any limit's percent remaining is below threshold.
func BelowThreshold(limits []Limit, threshold float64) bool {
	for _, l := range limits {
		if l.Pct < threshold {
			return true
		}
	}
	return false
}

type jsonEntry struct {
	Resource         string          `json:"resource"`
	Remaining        int             `json:"remaining"`
	Limit            int             `json:"limit"`
	ResetTimestamp   int64           `json:"reset_timestamp"`
	ResetIn          string          `json:"reset_in"`
	PercentRemaining json.RawMessage `json:"percent_remaining"`
}

// RenderJSON writes the processed limits as a pretty-printed JSON array. The
// percent_remaining field is rendered with exactly one decimal place.
func RenderJSON(w io.Writer, limits []Limit, now time.Time) error {
	entries := make([]jsonEntry, 0, len(limits))
	for _, l := range limits {
		pct := strconv.FormatFloat(l.Pct, 'f', 1, 64)
		entries = append(entries, jsonEntry{
			Resource:         l.Name,
			Remaining:        l.Remaining,
			Limit:            l.Limit,
			ResetTimestamp:   l.Reset,
			ResetIn:          FormatReset(l.Reset, now),
			PercentRemaining: json.RawMessage(pct),
		})
	}
	out, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(w, string(out))
	return err
}

// RenderTable writes the limits as an aligned, optionally colorized table.
// Reset times are computed relative to now.
func RenderTable(w io.Writer, limits []Limit, now time.Time, colorEnabled bool) {
	if len(limits) == 0 {
		fmt.Fprintln(w, "No rate limits to display.")
		return
	}
	nameWidth := 0
	for _, l := range limits {
		if len(l.Name) > nameWidth {
			nameWidth = len(l.Name)
		}
	}

	fmt.Fprintln(w)
	fmt.Fprintf(w, "  %s   %s   %s   %s\n",
		center("Resource", nameWidth),
		center("Remaining", 19),
		center("Meter", 20),
		fmt.Sprintf("%10s", "Resets in"),
	)
	fmt.Fprintf(w, "  %s   %s   %s   %s\n",
		strings.Repeat("-", nameWidth),
		strings.Repeat("-", 19),
		strings.Repeat("-", 20),
		strings.Repeat("-", 10),
	)

	for _, l := range limits {
		bar := Bar(l.Remaining, l.Limit, colorEnabled)
		resetStr := FormatReset(l.Reset, now)
		namePadded := fmt.Sprintf("%-*s", nameWidth, l.Name)
		nameDisplay := namePadded
		if colorEnabled {
			if l.Pct < 20 {
				nameDisplay = "\033[31m" + namePadded + "\033[0m"
			} else if l.Pct < 50 {
				nameDisplay = "\033[33m" + namePadded + "\033[0m"
			}
		}
		remStr := fmt.Sprintf("%d/%d  %3d%%", l.Remaining, l.Limit, int(l.Pct))
		fmt.Fprintf(w, "  %s   %19s   %s   %10s\n", nameDisplay, remStr, bar, resetStr)
	}
	fmt.Fprintln(w)
}
