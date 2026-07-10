// Package haproxyruntime provides the small subset of HAProxy's Runtime API
// needed by Berghain's operational helpers.
package haproxyruntime

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

const (
	defaultTimeout            = 5 * time.Second
	defaultTransactionTimeout = 2 * time.Minute
	maxResponseSize           = 1 << 20
	maxTransactionEntries     = 20_000
)

// Client sends commands to an HAProxy Runtime API UNIX socket.
type Client struct {
	SocketPath         string
	Timeout            time.Duration
	TransactionTimeout time.Duration
}

// ReplaceMap atomically replaces a runtime map with entries. HAProxy creates a
// temporary version, receives every entry, and exposes it only after commit.
func (c Client) ReplaceMap(ctx context.Context, mapRef string, entries map[string]string) error {
	if len(entries) > maxTransactionEntries {
		return fmt.Errorf("map has %d entries; transaction limit is %d", len(entries), maxTransactionEntries)
	}
	transactionTimeout := c.TransactionTimeout
	if transactionTimeout <= 0 {
		transactionTimeout = defaultTransactionTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, transactionTimeout)
	defer cancel()

	if err := validateToken("map reference", mapRef); err != nil {
		return err
	}

	keys := make([]string, 0, len(entries))
	for key, value := range entries {
		if err := validateToken("map key", key); err != nil {
			return err
		}
		if err := validateToken("map value", value); err != nil {
			return err
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	response, err := c.command(ctx, "prepare map "+mapRef)
	if err != nil {
		return fmt.Errorf("prepare map: %w", err)
	}
	version, err := parseVersion(response)
	if err != nil {
		return fmt.Errorf("prepare map: %w", err)
	}

	transaction := "@" + strconv.FormatUint(version, 10)
	for _, key := range keys {
		command := strings.Join([]string{"add map", transaction, mapRef, key, entries[key]}, " ")
		if err := c.silentCommand(ctx, command); err != nil {
			return fmt.Errorf("add map entry %q: %w", key, err)
		}
	}

	if err := c.silentCommand(ctx, strings.Join([]string{"commit map", transaction, mapRef}, " ")); err != nil {
		return fmt.Errorf("commit map: %w", err)
	}
	return nil
}

func (c Client) silentCommand(ctx context.Context, command string) error {
	response, err := c.command(ctx, command)
	if err != nil {
		return err
	}
	if message := strings.TrimSpace(response); message != "" {
		return errors.New(message)
	}
	return nil
}

func (c Client) command(ctx context.Context, command string) (string, error) {
	if c.SocketPath == "" {
		return "", errors.New("runtime socket path is empty")
	}
	if strings.ContainsAny(command, "\r\n") {
		return "", errors.New("runtime command contains a newline")
	}

	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}

	conn, err := (&net.Dialer{Timeout: timeout}).DialContext(ctx, "unix", c.SocketPath)
	if err != nil {
		return "", err
	}
	defer conn.Close()

	deadline := time.Now().Add(timeout)
	if contextDeadline, ok := ctx.Deadline(); ok && contextDeadline.Before(deadline) {
		deadline = contextDeadline
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return "", err
	}

	if _, err := io.WriteString(conn, command+"\n"); err != nil {
		return "", err
	}
	unixConn, ok := conn.(*net.UnixConn)
	if !ok {
		return "", errors.New("runtime connection is not a UNIX socket")
	}
	if err := unixConn.CloseWrite(); err != nil {
		return "", err
	}

	response, err := io.ReadAll(io.LimitReader(conn, maxResponseSize+1))
	if err != nil {
		return "", err
	}
	if len(response) > maxResponseSize {
		return "", errors.New("runtime response exceeds size limit")
	}
	return string(response), nil
}

func parseVersion(response string) (uint64, error) {
	const prefix = "New version created:"
	value, ok := strings.CutPrefix(strings.TrimSpace(response), prefix)
	if !ok {
		return 0, fmt.Errorf("unexpected response %q", strings.TrimSpace(response))
	}
	version, err := strconv.ParseUint(strings.TrimSpace(value), 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid transaction version: %w", err)
	}
	return version, nil
}

func validateToken(name, value string) error {
	if value == "" {
		return fmt.Errorf("%s is empty", name)
	}
	if strings.Contains(value, "<<") || strings.IndexFunc(value, func(r rune) bool {
		return r == ';' || unicode.IsSpace(r) || unicode.IsControl(r)
	}) >= 0 {
		return fmt.Errorf("%s contains a command delimiter or whitespace", name)
	}
	return nil
}
