//go:build e2e

package e2e

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestReputationPeersMesh runs the reputation service (cmd/feedupdater) as one
// peer in a mesh with TWO HAProxy instances and asserts, via the admin
// sockets, that:
//
//   - banlist and CrowdSec decisions appear in the reputation stick-tables of
//     BOTH instances (live push over the peers protocol),
//   - the CrowdSec decision's duration is honored (timed entry update),
//   - a deleted decision is zeroed everywhere,
//   - the HAProxies still replicate their own tables through the mesh while
//     the daemon is a member (it acknowledges their updates), and
//   - a restarted HAProxy resyncs back to the full state.
func TestReputationPeersMesh(t *testing.T) {
	if _, err := exec.LookPath("haproxy"); err != nil {
		t.Skip("haproxy binary not available")
	}

	const (
		peerA    = "127.0.0.1:19100"
		peerB    = "127.0.0.1:19101"
		peerFeed = "127.0.0.1:19102"
		feA      = "127.0.0.1:19110"
		feB      = "127.0.0.1:19111"

		banlistIP  = "198.51.100.10"
		crowdsecIP = "198.51.100.20"
	)

	dir := t.TempDir()

	// --- fake CrowdSec LAPI ------------------------------------------------
	var (
		lapiMu      sync.Mutex
		startupBody = `{"new":[{"scope":"Ip","value":"` + crowdsecIP + `","type":"ban","duration":"4h"}],"deleted":[]}`
		deltas      []string
	)
	lapi := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Api-Key") != "e2e-key" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		lapiMu.Lock()
		defer lapiMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Query().Get("startup") == "true" {
			io.WriteString(w, startupBody)
			return
		}
		if len(deltas) > 0 {
			io.WriteString(w, deltas[0])
			deltas = deltas[1:]
			return
		}
		io.WriteString(w, `{"new":[],"deleted":[]}`)
	}))
	defer lapi.Close()

	// --- reputation daemon --------------------------------------------------
	banlist := filepath.Join(dir, "banlist.txt")
	if err := os.WriteFile(banlist, []byte(banlistIP+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	feedBin := filepath.Join(dir, "feedupdater")
	build := exec.Command("go", "-C", "../..", "build", "-o", feedBin, "./cmd/feedupdater")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("building feedupdater: %v\n%s", err, out)
	}

	feed := exec.Command(feedBin,
		"-peer-listen", peerFeed,
		"-banlist", banlist,
		"-tor-exits=false",
		"-interval", "2s",
		"-crowdsec-url", lapi.URL,
		"-crowdsec-interval", "1s",
	)
	feed.Env = append(os.Environ(), "CROWDSEC_API_KEY=e2e-key")
	feedLog := &strings.Builder{}
	feed.Stdout, feed.Stderr = feedLog, feedLog
	if err := feed.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		_ = feed.Process.Kill()
		_, _ = feed.Process.Wait()
		if t.Failed() {
			t.Logf("feedupdater log:\n%s", feedLog.String())
		}
	}()

	// --- two HAProxy instances ----------------------------------------------
	haproxyCfg := func(sock, fe string) string {
		return `
global
    stats socket ` + sock + ` level admin
    log stdout format raw local0

defaults
    mode http
    timeout client 5s
    timeout server 5s
    timeout connect 5s

peers test_peers
    peer haproxy_a ` + peerA + `
    peer haproxy_b ` + peerB + `
    peer berghain_feed ` + peerFeed + `

frontend fe
    bind ` + fe + `
    http-request track-sc1 src table st_visits
    http-request return status 200 content-type "text/plain" string "ok"

backend st_visits
    stick-table type ip size 1m expire 10m store http_req_cnt peers test_peers

backend st_reputation_v4
    stick-table type ip size 1m expire 24h store gpt0 peers test_peers

backend st_reputation_v6
    stick-table type ipv6 size 1m expire 24h store gpt0 peers test_peers
`
	}

	sockA := filepath.Join(dir, "a.sock")
	sockB := filepath.Join(dir, "b.sock")
	cfgA := filepath.Join(dir, "a.cfg")
	cfgB := filepath.Join(dir, "b.cfg")
	if err := os.WriteFile(cfgA, []byte(haproxyCfg(sockA, feA)), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgB, []byte(haproxyCfg(sockB, feB)), 0o644); err != nil {
		t.Fatal(err)
	}

	startHAProxy := func(localPeer, cfg string) *exec.Cmd {
		t.Helper()
		cmd := exec.Command("haproxy", "-db", "-L", localPeer, "-f", cfg)
		log := &strings.Builder{}
		cmd.Stdout, cmd.Stderr = log, log
		if err := cmd.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			_ = cmd.Process.Kill()
			_, _ = cmd.Process.Wait()
			if t.Failed() {
				t.Logf("haproxy %s log:\n%s", localPeer, log.String())
			}
		})
		return cmd
	}
	haproxyA := startHAProxy("haproxy_a", cfgA)
	startHAProxy("haproxy_b", cfgB)

	// --- helpers --------------------------------------------------------------
	showTable := func(sock, table string) string {
		c, err := net.Dial("unix", sock)
		if err != nil {
			return ""
		}
		defer c.Close()
		_ = c.SetDeadline(time.Now().Add(2 * time.Second))
		fmt.Fprintf(c, "show table %s\n", table)
		b, _ := io.ReadAll(c)
		return string(b)
	}

	// entryLine returns the table line for a key, if present.
	entryLine := func(sock, table, key string) (string, bool) {
		for _, line := range strings.Split(showTable(sock, table), "\n") {
			if strings.Contains(line, "key="+key+" ") {
				return line, true
			}
		}
		return "", false
	}

	waitFor := func(what string, cond func() bool) {
		t.Helper()
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			if cond() {
				return
			}
			time.Sleep(250 * time.Millisecond)
		}
		t.Fatalf("timed out waiting for %s", what)
	}

	hasValue := func(sock, key, gpt0 string) bool {
		line, ok := entryLine(sock, "st_reputation_v4", key)
		return ok && strings.Contains(line, "gpt0="+gpt0)
	}

	// --- assertions -----------------------------------------------------------

	// 1. Both feeds reach both HAProxy instances.
	for _, sock := range []string{sockA, sockB} {
		waitFor("banlist entry on "+sock, func() bool { return hasValue(sock, banlistIP, "1") })
		waitFor("crowdsec entry on "+sock, func() bool { return hasValue(sock, crowdsecIP, "1") })
	}

	// 2. The CrowdSec decision's 4h duration is honored via a timed update:
	// its expiry must sit well below the 24h table default.
	line, _ := entryLine(sockA, "st_reputation_v4", crowdsecIP)
	exp := 0
	for _, f := range strings.Fields(line) {
		if v, ok := strings.CutPrefix(f, "exp="); ok {
			exp, _ = strconv.Atoi(v)
		}
	}
	if exp <= 0 || exp > int((4*time.Hour+time.Minute).Milliseconds()) {
		t.Fatalf("crowdsec entry expiry = %dms, want ~4h (timed update): %q", exp, line)
	}

	// 3. The HAProxies still replicate their own tables through the mesh while
	// the daemon is connected as a peer.
	if _, err := http.Get("http://" + feA + "/"); err != nil {
		t.Fatal(err)
	}
	waitFor("st_visits replication a->b", func() bool {
		_, ok := entryLine(sockB, "st_visits", "127.0.0.1")
		return ok
	})

	// 4. A deleted decision is zeroed on both instances.
	lapiMu.Lock()
	startupBody = `{"new":[],"deleted":[]}`
	deltas = append(deltas, `{"new":[],"deleted":[{"scope":"Ip","value":"`+crowdsecIP+`","type":"ban","duration":"-1s"}]}`)
	lapiMu.Unlock()
	for _, sock := range []string{sockA, sockB} {
		waitFor("crowdsec delete on "+sock, func() bool { return hasValue(sock, crowdsecIP, "0") })
	}

	// 5. A restarted HAProxy resyncs the reputation state from the mesh.
	_ = haproxyA.Process.Kill()
	_, _ = haproxyA.Process.Wait()
	startHAProxy("haproxy_a", cfgA)
	waitFor("banlist entry after restart", func() bool { return hasValue(sockA, banlistIP, "1") })
}
