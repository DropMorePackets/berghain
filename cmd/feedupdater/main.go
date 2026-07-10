// Command feedupdater builds and optionally applies an HAProxy IP reputation map.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/DropMorePackets/berghain/internal/haproxyruntime"
)

const maxFeedBytes = 16 << 20

const maxReputationEntries = 20_000

const (
	runtimeUpdateTimeout   = 2 * time.Minute
	runtimeRollbackTimeout = 30 * time.Second
)

type reputationAction uint8

const (
	actionOff       reputationAction = 0
	actionBlock     reputationAction = 1
	actionChallenge reputationAction = 3
)

func parseAction(value string) (reputationAction, error) {
	switch value {
	case "off":
		return actionOff, nil
	case "block":
		return actionBlock, nil
	case "challenge":
		return actionChallenge, nil
	default:
		return actionOff, fmt.Errorf("unknown action %q (want off, block, or challenge)", value)
	}
}

type sourceKind uint8

const (
	sourceAddresses sourceKind = iota
	sourcePrefixes
)

type source struct {
	name    string
	urls    []string
	kind    sourceKind
	action  reputationAction
	minimum int
}

type config struct {
	mapFile       string
	runtimeSocket string
	runtimeMap    string
	banlist       string
	sources       []source
}

func defaultSources(torAction, cloudflareAction reputationAction) []source {
	return []source{
		{
			name:    "tor exits",
			urls:    []string{"https://check.torproject.org/torbulkexitlist"},
			kind:    sourceAddresses,
			action:  torAction,
			minimum: 1_000,
		},
		{
			name: "Cloudflare ranges",
			urls: []string{
				"https://www.cloudflare.com/ips-v4",
				"https://www.cloudflare.com/ips-v6",
			},
			kind:    sourcePrefixes,
			action:  cloudflareAction,
			minimum: 20,
		},
	}
}

func fetch(ctx context.Context, client *http.Client, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "berghain-feedupdater/1")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected HTTP status %d", resp.StatusCode)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxFeedBytes {
		return nil, fmt.Errorf("response exceeds %d bytes", maxFeedBytes)
	}
	return body, nil
}

func parseAddresses(body []byte) ([]string, error) {
	return parseLines(body, func(value string) (string, error) {
		address, err := netip.ParseAddr(value)
		if err != nil {
			return "", err
		}
		if address.Zone() != "" {
			return "", errors.New("scoped IPv6 addresses are not valid map keys")
		}
		return address.Unmap().String(), nil
	})
}

func parsePrefixes(body []byte) ([]string, error) {
	return parseLines(body, func(value string) (string, error) {
		prefix, err := netip.ParsePrefix(value)
		if err != nil {
			return "", err
		}
		if masked := prefix.Masked(); prefix != masked {
			return "", fmt.Errorf("prefix has host bits set; use %s", masked)
		}
		return prefix.String(), nil
	})
}

func parseLines(body []byte, parse func(string) (string, error)) ([]string, error) {
	values := make(map[string]struct{})
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if len(strings.Fields(line)) != 1 {
			return nil, fmt.Errorf("line %d contains extra fields", lineNumber)
		}
		value, err := parse(line)
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", lineNumber, err)
		}
		values[value] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}

	result := make([]string, 0, len(values))
	for value := range values {
		result = append(result, value)
	}
	sort.Strings(result)
	return result, nil
}

func readLimitedFile(path string) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	body, err := io.ReadAll(io.LimitReader(file, maxFeedBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxFeedBytes {
		return nil, fmt.Errorf("file exceeds %d bytes", maxFeedBytes)
	}
	return body, nil
}

func mergeEntry(entries map[string]reputationAction, key string, action reputationAction) {
	current := entries[key]
	if current == actionBlock || action == actionBlock {
		entries[key] = actionBlock
		return
	}
	if action > current {
		entries[key] = action
	}
}

func keyPrefix(key string) (netip.Prefix, error) {
	if prefix, err := netip.ParsePrefix(key); err == nil {
		return prefix, nil
	}
	address, err := netip.ParseAddr(key)
	if err != nil {
		return netip.Prefix{}, err
	}
	return netip.PrefixFrom(address, address.BitLen()), nil
}

func applyBlockPrecedence(entries map[string]reputationAction) error {
	var blocked []netip.Prefix
	for key, action := range entries {
		if action != actionBlock {
			continue
		}
		prefix, err := keyPrefix(key)
		if err != nil {
			return fmt.Errorf("invalid block key %q: %w", key, err)
		}
		blocked = append(blocked, prefix)
	}

	for key, action := range entries {
		if action != actionChallenge {
			continue
		}
		prefix, err := keyPrefix(key)
		if err != nil {
			return fmt.Errorf("invalid challenge key %q: %w", key, err)
		}
		for _, block := range blocked {
			if block.Addr().BitLen() == prefix.Addr().BitLen() &&
				block.Bits() <= prefix.Bits() && block.Contains(prefix.Addr()) {
				entries[key] = actionBlock
				break
			}
		}
	}
	return nil
}

func collect(ctx context.Context, client *http.Client, cfg config) (map[string]reputationAction, error) {
	entries := make(map[string]reputationAction)
	enabled := false
	for _, source := range cfg.sources {
		if source.action == actionOff {
			continue
		}
		enabled = true
		var sourceValues []string
		for _, url := range source.urls {
			body, err := fetch(ctx, client, url)
			if err != nil {
				return nil, fmt.Errorf("fetch %s from %s: %w", source.name, url, err)
			}

			var values []string
			switch source.kind {
			case sourceAddresses:
				values, err = parseAddresses(body)
			case sourcePrefixes:
				values, err = parsePrefixes(body)
			default:
				err = errors.New("unknown source kind")
			}
			if err != nil {
				return nil, fmt.Errorf("parse %s from %s: %w", source.name, url, err)
			}
			if len(values) == 0 {
				return nil, fmt.Errorf("parse %s from %s: no entries", source.name, url)
			}
			sourceValues = append(sourceValues, values...)
		}
		if len(sourceValues) < source.minimum {
			return nil, fmt.Errorf("parse %s: got %d entries, require at least %d", source.name, len(sourceValues), source.minimum)
		}
		for _, value := range sourceValues {
			mergeEntry(entries, value, source.action)
		}
	}

	if cfg.banlist != "" {
		enabled = true
		body, err := readLimitedFile(cfg.banlist)
		if err != nil {
			return nil, fmt.Errorf("read banlist: %w", err)
		}
		values, err := parseAddresses(body)
		if err != nil {
			return nil, fmt.Errorf("parse banlist: %w", err)
		}
		for _, value := range values {
			mergeEntry(entries, value, actionBlock)
		}
	}

	if !enabled {
		return nil, errors.New("no reputation sources are enabled")
	}
	if err := applyBlockPrecedence(entries); err != nil {
		return nil, err
	}
	if len(entries) > maxReputationEntries {
		return nil, fmt.Errorf("reputation map has %d entries; limit is %d", len(entries), maxReputationEntries)
	}
	return entries, nil
}

func mapEntries(entries map[string]reputationAction) map[string]string {
	result := make(map[string]string, len(entries))
	for key, action := range entries {
		result[key] = strconv.FormatUint(uint64(action), 10)
	}
	return result
}

func renderMap(entries map[string]string) ([]byte, error) {
	var output strings.Builder
	_, _ = output.WriteString("# Generated by cmd/feedupdater. Do not edit.\n")
	keys := make([]string, 0, len(entries))
	for key := range entries {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		if _, err := fmt.Fprintf(&output, "%s %s\n", key, entries[key]); err != nil {
			return nil, err
		}
	}
	return []byte(output.String()), nil
}

func writeFileAtomic(path string, body []byte) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(directory, ".reputation-map-*")
	if err != nil {
		return err
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)

	mode := os.FileMode(0o644)
	uid, gid := -1, -1
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
		if stat, ok := info.Sys().(*syscall.Stat_t); ok {
			uid, gid = int(stat.Uid), int(stat.Gid)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		temporary.Close()
		return err
	}
	if uid >= 0 {
		if err := temporary.Chown(uid, gid); err != nil {
			temporary.Close()
			return err
		}
	}
	if err := temporary.Chmod(mode); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(body); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return err
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	if err := os.Rename(temporaryPath, path); err != nil {
		return err
	}
	return nil
}

func writeMapAtomic(path string, entries map[string]string) error {
	body, err := renderMap(entries)
	if err != nil {
		return err
	}
	return writeFileAtomic(path, body)
}

func readMap(path string) ([]byte, map[string]string, bool, error) {
	body, err := readLimitedFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, map[string]string{}, false, nil
	}
	if err != nil {
		return nil, nil, false, err
	}

	entries := make(map[string]string)
	scanner := bufio.NewScanner(strings.NewReader(string(body)))
	for lineNumber := 1; scanner.Scan(); lineNumber++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, nil, false, fmt.Errorf("map line %d: want key and value", lineNumber)
		}
		if _, duplicate := entries[fields[0]]; duplicate {
			return nil, nil, false, fmt.Errorf("map line %d: duplicate key %q", lineNumber, fields[0])
		}
		entries[fields[0]] = fields[1]
	}
	if err := scanner.Err(); err != nil {
		return nil, nil, false, err
	}
	return body, entries, true, nil
}

func acquireMapLock(path string) (*os.File, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	lock, err := os.OpenFile(path+".lock", os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		lock.Close()
		return nil, fmt.Errorf("another updater owns %s: %w", path, err)
	}
	return lock, nil
}

func releaseMapLock(lock *os.File) {
	_ = syscall.Flock(int(lock.Fd()), syscall.LOCK_UN)
	_ = lock.Close()
}

func restoreMap(path string, body []byte, existed bool) error {
	if existed {
		return writeFileAtomic(path, body)
	}
	err := os.Remove(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	return err
}

func update(ctx context.Context, client *http.Client, cfg config) (int, error) {
	collected, err := collect(ctx, client, cfg)
	if err != nil {
		return 0, err
	}
	entries := mapEntries(collected)

	lock, err := acquireMapLock(cfg.mapFile)
	if err != nil {
		return 0, fmt.Errorf("lock map: %w", err)
	}
	defer releaseMapLock(lock)

	previousBody, previousEntries, previousExists, err := readMap(cfg.mapFile)
	if err != nil {
		return 0, fmt.Errorf("read previous map: %w", err)
	}
	if cfg.runtimeSocket != "" && !previousExists {
		return 0, errors.New("runtime updates require an existing persistent map for rollback")
	}
	if err := writeMapAtomic(cfg.mapFile, entries); err != nil {
		return 0, fmt.Errorf("write map: %w", err)
	}

	if cfg.runtimeSocket != "" {
		mapRef := cfg.runtimeMap
		if mapRef == "" {
			mapRef = cfg.mapFile
		}
		runtime := haproxyruntime.Client{SocketPath: cfg.runtimeSocket}
		runtimeCtx, cancelRuntime := context.WithTimeout(ctx, runtimeUpdateTimeout)
		err := runtime.ReplaceMap(runtimeCtx, mapRef, entries)
		cancelRuntime()
		if err != nil {
			applyErr := fmt.Errorf("apply runtime map: %w", err)
			rollbackCtx, cancelRollback := context.WithTimeout(context.Background(), runtimeRollbackTimeout)
			runtimeRollbackErr := runtime.ReplaceMap(rollbackCtx, mapRef, previousEntries)
			cancelRollback()
			fileRollbackErr := restoreMap(cfg.mapFile, previousBody, previousExists)
			if runtimeRollbackErr != nil {
				applyErr = errors.Join(applyErr, fmt.Errorf("rollback runtime map: %w", runtimeRollbackErr))
			}
			if fileRollbackErr != nil {
				applyErr = errors.Join(applyErr, fmt.Errorf("rollback persistent map: %w", fileRollbackErr))
			}
			return 0, applyErr
		}
	}
	return len(entries), nil
}

func main() {
	var (
		mapFile         string
		runtimeSocket   string
		runtimeMap      string
		banlist         string
		torActionName   string
		cloudActionName string
		interval        time.Duration
	)
	flag.StringVar(&mapFile, "map-file", "examples/haproxy/maps/reputation.map", "persistent HAProxy reputation map")
	flag.StringVar(&runtimeSocket, "runtime-socket", "", "optional HAProxy Runtime API UNIX socket")
	flag.StringVar(&runtimeMap, "runtime-map", "", "map path as registered in HAProxy (defaults to -map-file)")
	flag.StringVar(&banlist, "banlist", "", "optional local file of IPs to block")
	flag.StringVar(&torActionName, "tor-action", "challenge", "Tor exit policy: off, block, or challenge")
	flag.StringVar(&cloudActionName, "cloudflare-action", "off", "Cloudflare range policy: off, block, or challenge")
	flag.DurationVar(&interval, "interval", 0, "refresh interval; zero runs once")
	flag.Parse()

	torAction, err := parseAction(torActionName)
	if err != nil {
		slog.Error("invalid Tor action", "error", err)
		os.Exit(2)
	}
	cloudAction, err := parseAction(cloudActionName)
	if err != nil {
		slog.Error("invalid Cloudflare action", "error", err)
		os.Exit(2)
	}

	cfg := config{
		mapFile:       mapFile,
		runtimeSocket: runtimeSocket,
		runtimeMap:    runtimeMap,
		banlist:       banlist,
		sources:       defaultSources(torAction, cloudAction),
	}
	client := &http.Client{Timeout: 30 * time.Second}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	run := func() bool {
		count, err := update(ctx, client, cfg)
		if err != nil {
			slog.Error("reputation update failed; preserving the previous runtime map", "error", err)
			return false
		}
		slog.Info("reputation map updated", "entries", count, "path", mapFile)
		return true
	}

	if ok := run(); !ok && interval <= 0 {
		os.Exit(1)
	}
	if interval <= 0 {
		return
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			run()
		}
	}
}
