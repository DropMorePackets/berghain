package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestParseAddresses(t *testing.T) {
	got, err := parseAddresses([]byte("# comment\n1.2.3.4\n2001:db8::1\n1.2.3.4\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"1.2.3.4", "2001:db8::1"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("addresses = %#v, want %#v", got, want)
	}
	if _, err := parseAddresses([]byte("1.2.3.4\nnot-an-ip\n")); err == nil {
		t.Fatal("invalid address was accepted")
	}
	if _, err := parseAddresses([]byte("fe80::1%eth0\n")); err == nil {
		t.Fatal("scoped IPv6 address was accepted")
	}
}

func TestParsePrefixes(t *testing.T) {
	got, err := parsePrefixes([]byte("10.0.0.0/8\n2001:db8::/32\n"))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"10.0.0.0/8", "2001:db8::/32"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("prefixes = %#v, want %#v", got, want)
	}
	if _, err := parsePrefixes([]byte("10.0.0.0/8 injected\n")); err == nil {
		t.Fatal("extra fields were accepted")
	}
	if _, err := parsePrefixes([]byte("10.0.1.2/8\n")); err == nil {
		t.Fatal("non-canonical prefix was widened")
	}
}

func TestCollectAndBlockPrecedence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/tor":
			_, _ = w.Write([]byte("1.2.3.4\n2001:db8::1\n"))
		case "/ranges":
			_, _ = w.Write([]byte("10.0.0.0/8\n"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	banlist := filepath.Join(t.TempDir(), "bans.txt")
	if err := os.WriteFile(banlist, []byte("1.2.3.4\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config{
		banlist: banlist,
		sources: []source{
			{name: "tor", urls: []string{server.URL + "/tor"}, kind: sourceAddresses, action: actionChallenge},
			{name: "ranges", urls: []string{server.URL + "/ranges"}, kind: sourcePrefixes, action: actionChallenge},
		},
	}
	got, err := collect(context.Background(), server.Client(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]reputationAction{
		"1.2.3.4":     actionBlock,
		"2001:db8::1": actionChallenge,
		"10.0.0.0/8":  actionChallenge,
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("entries = %#v, want %#v", got, want)
	}
}

func TestCollectAppliesBlockPrefixPrecedence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/block":
			_, _ = w.Write([]byte("10.0.0.0/8\n"))
		case "/challenge":
			_, _ = w.Write([]byte("10.1.2.3\n192.0.2.1\n"))
		}
	}))
	defer server.Close()

	cfg := config{sources: []source{
		{name: "blocked", urls: []string{server.URL + "/block"}, kind: sourcePrefixes, action: actionBlock},
		{name: "challenged", urls: []string{server.URL + "/challenge"}, kind: sourceAddresses, action: actionChallenge},
	}}
	got, err := collect(context.Background(), server.Client(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got["10.1.2.3"] != actionBlock {
		t.Fatalf("address inside blocked prefix has action %d", got["10.1.2.3"])
	}
	if got["192.0.2.1"] != actionChallenge {
		t.Fatalf("unrelated address has action %d", got["192.0.2.1"])
	}
}

func TestUpdatePreservesMapOnSourceFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer server.Close()

	mapFile := filepath.Join(t.TempDir(), "reputation.map")
	const previous = "1.2.3.4 1\n"
	if err := os.WriteFile(mapFile, []byte(previous), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config{
		mapFile: mapFile,
		sources: []source{{
			name: "broken", urls: []string{server.URL}, kind: sourceAddresses, action: actionChallenge,
		}},
	}
	if _, err := update(context.Background(), server.Client(), cfg); err == nil {
		t.Fatal("update succeeded with a failed source")
	}
	body, err := os.ReadFile(mapFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != previous {
		t.Fatalf("map changed after failed update: %q", body)
	}
}

func TestUpdateRejectsEmptyRemoteSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# no entries\n"))
	}))
	defer server.Close()

	mapFile := filepath.Join(t.TempDir(), "reputation.map")
	cfg := config{
		mapFile: mapFile,
		sources: []source{{
			name: "empty", urls: []string{server.URL}, kind: sourceAddresses, action: actionChallenge,
		}},
	}
	if _, err := update(context.Background(), server.Client(), cfg); err == nil {
		t.Fatal("empty source was accepted")
	}
	if _, err := os.Stat(mapFile); !os.IsNotExist(err) {
		t.Fatalf("map created after empty update: %v", err)
	}
}

func TestCollectRejectsImplausiblySmallSource(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("1.2.3.4\n"))
	}))
	defer server.Close()

	cfg := config{sources: []source{{
		name: "truncated", urls: []string{server.URL}, kind: sourceAddresses,
		action: actionChallenge, minimum: 2,
	}}}
	if _, err := collect(context.Background(), server.Client(), cfg); err == nil {
		t.Fatal("implausibly small source was accepted")
	}
}

func TestUpdateRollsBackFileWhenRuntimeUpdateFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("5.6.7.8\n"))
	}))
	defer server.Close()

	directory := t.TempDir()
	mapFile := filepath.Join(directory, "reputation.map")
	const previous = "# previous\n1.2.3.4 1\n"
	if err := os.WriteFile(mapFile, []byte(previous), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := config{
		mapFile:       mapFile,
		runtimeSocket: filepath.Join(directory, "missing.sock"),
		runtimeMap:    "reputation.map",
		sources: []source{{
			name: "new", urls: []string{server.URL}, kind: sourceAddresses, action: actionChallenge,
		}},
	}
	if _, err := update(context.Background(), server.Client(), cfg); err == nil {
		t.Fatal("runtime failure was ignored")
	}
	body, err := os.ReadFile(mapFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != previous {
		t.Fatalf("persistent map was not rolled back: %q", body)
	}
}

func TestUpdateRejectsConcurrentOwner(t *testing.T) {
	directory := t.TempDir()
	mapFile := filepath.Join(directory, "reputation.map")
	banlist := filepath.Join(directory, "bans.txt")
	const previous = "# previous\n1.2.3.4 1\n"
	if err := os.WriteFile(mapFile, []byte(previous), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(banlist, []byte("5.6.7.8\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	lock, err := acquireMapLock(mapFile)
	if err != nil {
		t.Fatal(err)
	}
	defer releaseMapLock(lock)

	cfg := config{mapFile: mapFile, banlist: banlist}
	if _, err := update(context.Background(), http.DefaultClient, cfg); err == nil {
		t.Fatal("concurrent update was accepted")
	}
	body, err := os.ReadFile(mapFile)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != previous {
		t.Fatalf("map changed during rejected concurrent update: %q", body)
	}
}

func TestWriteMapAtomic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "new", "maps", "reputation.map")
	entries := map[string]string{"2001:db8::1": "3", "1.2.3.4": "1"}
	if err := writeMapAtomic(path, entries); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(string(body)), "\n")
	want := []string{
		"# Generated by cmd/feedupdater. Do not edit.",
		"1.2.3.4 1",
		"2001:db8::1 3",
	}
	if !reflect.DeepEqual(lines, want) {
		t.Fatalf("map lines = %#v, want %#v", lines, want)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o644 {
		t.Fatalf("map permissions = %o, want 644", got)
	}
}

func TestWriteMapAtomicPreservesMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reputation.map")
	if err := os.WriteFile(path, []byte("old\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := writeMapAtomic(path, map[string]string{"1.2.3.4": "1"}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("map permissions = %o, want 600", got)
	}
}
