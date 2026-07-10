package haproxyruntime

import (
	"bufio"
	"context"
	"net"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

type fakeRuntime struct {
	t        *testing.T
	listener net.Listener

	mu       sync.Mutex
	commands []string
	failOn   string
	wg       sync.WaitGroup
}

func newFakeRuntime(t *testing.T, failOn string) *fakeRuntime {
	t.Helper()
	listener, err := net.Listen("unix", filepath.Join(t.TempDir(), "haproxy.sock"))
	if err != nil {
		t.Fatal(err)
	}
	fake := &fakeRuntime{t: t, listener: listener, failOn: failOn}
	fake.wg.Add(1)
	go fake.serve()
	t.Cleanup(func() {
		_ = listener.Close()
		fake.wg.Wait()
	})
	return fake
}

func (f *fakeRuntime) serve() {
	defer f.wg.Done()
	for {
		conn, err := f.listener.Accept()
		if err != nil {
			return
		}
		f.wg.Add(1)
		go func() {
			defer f.wg.Done()
			defer conn.Close()

			command, err := bufio.NewReader(conn).ReadString('\n')
			if err != nil {
				f.t.Errorf("reading command: %v", err)
				return
			}
			command = strings.TrimSpace(command)
			f.mu.Lock()
			f.commands = append(f.commands, command)
			f.mu.Unlock()

			switch {
			case strings.Contains(command, f.failOn) && f.failOn != "":
				_, _ = conn.Write([]byte("simulated runtime failure\n"))
			case strings.HasPrefix(command, "prepare map "):
				_, _ = conn.Write([]byte("New version created: 17\n"))
			}
		}()
	}
}

func (f *fakeRuntime) socketPath() string {
	return f.listener.Addr().String()
}

func (f *fakeRuntime) received() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.commands...)
}

func TestReplaceMap(t *testing.T) {
	fake := newFakeRuntime(t, "")
	client := Client{SocketPath: fake.socketPath(), Timeout: time.Second}

	err := client.ReplaceMap(context.Background(), "reputation.map", map[string]string{
		"2001:db8::1": "3",
		"1.2.3.4":     "1",
	})
	if err != nil {
		t.Fatal(err)
	}

	want := []string{
		"prepare map reputation.map",
		"add map @17 reputation.map 1.2.3.4 1",
		"add map @17 reputation.map 2001:db8::1 3",
		"commit map @17 reputation.map",
	}
	if got := fake.received(); !reflect.DeepEqual(got, want) {
		t.Fatalf("commands = %#v, want %#v", got, want)
	}
}

func TestReplaceMapDoesNotCommitFailedTransaction(t *testing.T) {
	fake := newFakeRuntime(t, "bad-key")
	client := Client{SocketPath: fake.socketPath(), Timeout: time.Second}

	err := client.ReplaceMap(context.Background(), "reputation.map", map[string]string{
		"bad-key": "1",
	})
	if err == nil || !strings.Contains(err.Error(), "simulated runtime failure") {
		t.Fatalf("ReplaceMap error = %v", err)
	}

	for _, command := range fake.received() {
		if strings.HasPrefix(command, "commit map ") {
			t.Fatalf("failed transaction was committed: %q", command)
		}
	}
}

func TestReplaceMapRejectsCommandInjection(t *testing.T) {
	client := Client{SocketPath: "unused"}
	for _, test := range []struct {
		name    string
		mapRef  string
		entries map[string]string
	}{
		{name: "map", mapRef: "bad map", entries: map[string]string{"key": "1"}},
		{name: "key", mapRef: "map", entries: map[string]string{"key\nshow info": "1"}},
		{name: "value", mapRef: "map", entries: map[string]string{"key": "1 2"}},
		{name: "delimiter", mapRef: "map;show info", entries: map[string]string{"key": "1"}},
		{name: "control", mapRef: "map", entries: map[string]string{"key\x00": "1"}},
		{name: "payload", mapRef: "map", entries: map[string]string{"<<": "1"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			if err := client.ReplaceMap(context.Background(), test.mapRef, test.entries); err == nil {
				t.Fatal("ReplaceMap accepted an unsafe token")
			}
		})
	}
}

func TestReplaceMapRejectsOversizedTransaction(t *testing.T) {
	entries := make(map[string]string, maxTransactionEntries+1)
	for i := 0; i <= maxTransactionEntries; i++ {
		entries[strconv.Itoa(i)] = "1"
	}
	client := Client{SocketPath: "unused"}
	if err := client.ReplaceMap(context.Background(), "map", entries); err == nil {
		t.Fatal("oversized transaction was accepted")
	}
}

func TestParseVersion(t *testing.T) {
	if got, err := parseVersion("\nNew version created: 42\n"); err != nil || got != 42 {
		t.Fatalf("parseVersion = %d, %v", got, err)
	}
	if _, err := parseVersion("permission denied"); err == nil {
		t.Fatal("parseVersion accepted an error response")
	}
}
