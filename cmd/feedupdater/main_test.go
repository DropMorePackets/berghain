package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLines(t *testing.T) {
	cf := "# comment\n173.245.48.0/20\n\n  103.21.244.0/22  \n"
	got := parseLines(cf, cidrOnly)
	want := []string{"173.245.48.0/20", "103.21.244.0/22"}
	if len(got) != len(want) {
		t.Fatalf("cidrOnly parsed %d lines, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}

	tor := "1.2.3.4\n# note\n5.6.7.8\n"
	gotTor := parseLines(tor, ipToMapEntry)
	wantTor := []string{"1.2.3.4 1", "5.6.7.8 1"}
	for i := range wantTor {
		if i >= len(gotTor) || gotTor[i] != wantTor[i] {
			t.Errorf("tor line %d = %q, want %q", i, gotTor[i:], wantTor[i])
		}
	}
}

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tor_exit.map")

	if err := writeAtomic(path, []string{"1.2.3.4 1", "5.6.7.8 1"}); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	got := string(b)
	for _, want := range []string{"1.2.3.4 1", "5.6.7.8 1", "# Generated"} {
		if !contains(got, want) {
			t.Errorf("output missing %q; got:\n%s", want, got)
		}
	}

	// Overwriting must fully replace the previous contents.
	if err := writeAtomic(path, []string{"9.9.9.9 1"}); err != nil {
		t.Fatalf("second writeAtomic: %v", err)
	}
	b, _ = os.ReadFile(path)
	if contains(string(b), "1.2.3.4") {
		t.Errorf("stale entry survived rewrite:\n%s", b)
	}

	// No leftover temp files.
	entries, _ := os.ReadDir(dir)
	if len(entries) != 1 {
		t.Errorf("expected 1 file after atomic writes, found %d", len(entries))
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
