package main

import (
	"net/netip"
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
}

func TestParseIPs(t *testing.T) {
	body := "1.2.3.4\n# comment\n\n  5.6.7.8  \n2001:db8::1\nnot-an-ip\n"
	got := parseIPs(body)
	want := []netip.Addr{
		netip.MustParseAddr("1.2.3.4"),
		netip.MustParseAddr("5.6.7.8"),
		netip.MustParseAddr("2001:db8::1"),
	}
	if len(got) != len(want) {
		t.Fatalf("parseIPs returned %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("ip %d = %v, want %v", i, got[i], want[i])
		}
	}
}

func TestWriteAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "cloudflare-ips.lst")

	if err := writeAtomic(path, []string{"1.2.3.0/24", "5.6.7.0/24"}); err != nil {
		t.Fatalf("writeAtomic: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("reading result: %v", err)
	}
	got := string(b)
	for _, want := range []string{"1.2.3.0/24", "5.6.7.0/24", "# Generated"} {
		if !contains(got, want) {
			t.Errorf("output missing %q; got:\n%s", want, got)
		}
	}

	if err := writeAtomic(path, []string{"9.9.9.0/24"}); err != nil {
		t.Fatalf("second writeAtomic: %v", err)
	}
	b, _ = os.ReadFile(path)
	if contains(string(b), "1.2.3.0") {
		t.Errorf("stale entry survived rewrite:\n%s", b)
	}

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
