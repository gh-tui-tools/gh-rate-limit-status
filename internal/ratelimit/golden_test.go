package ratelimit

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var update = flag.Bool("update", false, "update golden files")

// goldenNow is the fixed clock for the golden tests so reset times are deterministic.
var goldenNow = time.Unix(1_000_000, 0)

func loadFixture(t *testing.T) []Resource {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "rate_limit.json"))
	if err != nil {
		t.Fatal(err)
	}
	res, err := ParseResources(data)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func checkGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("%s mismatch:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestGoldenTableColorOff(t *testing.T) {
	limits := Process(loadFixture(t), false)
	var buf bytes.Buffer
	RenderTable(&buf, limits, goldenNow, false)
	checkGolden(t, "table_coloroff.golden", buf.Bytes())
}

func TestGoldenJSON(t *testing.T) {
	limits := Process(loadFixture(t), false)
	var buf bytes.Buffer
	if err := RenderJSON(&buf, limits, goldenNow); err != nil {
		t.Fatal(err)
	}
	checkGolden(t, "output_json.golden", buf.Bytes())
}
