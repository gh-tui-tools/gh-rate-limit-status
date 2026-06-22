package main

import "testing"

func TestParseArgs(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		o, err := parseArgs(nil)
		if err != nil || o.help || o.all || o.json || o.watch || o.warn != nil {
			t.Fatalf("unexpected: %+v err=%v", o, err)
		}
	})
	t.Run("flags", func(t *testing.T) {
		o, err := parseArgs([]string{"-a", "--json", "-w", "-h"})
		if err != nil || !o.all || !o.json || !o.watch || !o.help {
			t.Fatalf("unexpected: %+v err=%v", o, err)
		}
	})
	t.Run("long all", func(t *testing.T) {
		o, _ := parseArgs([]string{"--all", "--watch", "--help"})
		if !o.all || !o.watch || !o.help {
			t.Fatalf("unexpected: %+v", o)
		}
	})
	t.Run("warn ok", func(t *testing.T) {
		o, err := parseArgs([]string{"--warn", "20"})
		if err != nil || o.warn == nil || *o.warn != 20 {
			t.Fatalf("unexpected: %+v err=%v", o, err)
		}
	})
	t.Run("warn missing", func(t *testing.T) {
		_, err := parseArgs([]string{"--warn"})
		if err == nil || err.Error() != "--warn requires a threshold percentage" {
			t.Fatalf("want missing-arg error, got %v", err)
		}
	})
	t.Run("warn nan", func(t *testing.T) {
		_, err := parseArgs([]string{"--warn", "abc"})
		if err == nil || err.Error() != "--warn requires a number" {
			t.Fatalf("want number error, got %v", err)
		}
	})
	t.Run("unknown ignored", func(t *testing.T) {
		o, err := parseArgs([]string{"--nope", "-x"})
		if err != nil || o.help || o.all {
			t.Fatalf("unknown flags should be ignored: %+v err=%v", o, err)
		}
	})
}
