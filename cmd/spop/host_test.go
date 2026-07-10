package main

import (
	"context"
	"net/http"
	"net/netip"
	"strings"
	"testing"
	"time"

	"github.com/dropmorepackets/haproxy-go/pkg/encoding"

	"github.com/DropMorePackets/berghain"
)

func TestHostWithoutPort(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "hostname", host: "example.com", want: "example.com"},
		{name: "hostname with port", host: "example.com:8443", want: "example.com"},
		{name: "hostname with zero port", host: "example.com:0", want: "example.com"},
		{name: "hostname with maximum port", host: "example.com:65535", want: "example.com"},
		{name: "hostname with leading zero port", host: "example.com:00443", want: "example.com"},
		{name: "hostname with nonnumeric port", host: "example.com:notaport", want: "example.com:notaport"},
		{name: "hostname with mixed port", host: "example.com:443x", want: "example.com:443x"},
		{name: "hostname with empty port", host: "example.com:", want: "example.com:"},
		{name: "hostname with out of range port", host: "example.com:65536", want: "example.com:65536"},
		{name: "empty host with port", host: ":443", want: ":443"},
		{name: "IPv4 with port", host: "192.0.2.1:8080", want: "192.0.2.1"},
		{name: "bracketed IPv6 with port", host: "[2001:db8::1]:443", want: "[2001:db8::1]"},
		{name: "bracketed IPv6 without port", host: "[2001:db8::1]", want: "[2001:db8::1]"},
		{name: "bracketed IPv6 with nonnumeric port", host: "[2001:db8::1]:https", want: "[2001:db8::1]:https"},
		{name: "bracketed IPv6 with mixed port", host: "[2001:db8::1]:443x", want: "[2001:db8::1]:443x"},
		{name: "bracketed IPv6 with empty port", host: "[2001:db8::1]:", want: "[2001:db8::1]:"},
		{name: "bracketed IPv6 with out of range port", host: "[2001:db8::1]:65536", want: "[2001:db8::1]:65536"},
		{name: "unbracketed IPv6", host: "2001:db8::1", want: "2001:db8::1"},
		{name: "ambiguous unbracketed IPv6 port", host: "2001:db8::1:443", want: "2001:db8::1:443"},
		{name: "malformed bracket suffix", host: "[::1]suffix", want: "[::1]suffix"},
		{name: "malformed bracketed port", host: "[::1]:443:extra", want: "[::1]:443:extra"},
		{name: "empty", host: "", want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(hostWithoutPort([]byte(tt.host))); got != tt.want {
				t.Fatalf("hostWithoutPort(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestNormalizeHost(t *testing.T) {
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "hostname", host: "Example.COM", want: "example.com"},
		{name: "hostname with port", host: "Sub.Example.COM:8443", want: "sub.example.com"},
		{name: "bracketed IPv6", host: "[2001:DB8::A]:443", want: "[2001:db8::a]"},
		{name: "malformed port stays part of identity", host: "Example.COM:HTTPS", want: "example.com:https"},
		{name: "non-ASCII bytes are preserved", host: "Ex\u00c4mple.COM", want: "ex\u00c4mple.com"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := string(normalizeHost([]byte(tt.host))); got != tt.want {
				t.Fatalf("normalizeHost(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestGetTrustedDomain(t *testing.T) {
	trustedDomains := []string{"Example.COM"}
	tests := []struct {
		name string
		host string
		want string
	}{
		{name: "exact", host: "example.com", want: "example.com"},
		{name: "case insensitive exact", host: "EXAMPLE.COM", want: "example.com"},
		{name: "case insensitive subdomain", host: "Sub.eXaMpLe.CoM", want: "example.com"},
		{name: "port stripped before match", host: "sub.example.com:8443", want: "example.com"},
		{name: "invalid port does not match", host: "sub.example.com:notaport"},
		{name: "lookalike suffix", host: "notexample.com"},
		{name: "parent suffix", host: "example.com.evil"},
		{name: "unrelated", host: "example.net"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			host := normalizeHost([]byte(tt.host))
			if got := string(getTrustedDomain(host, trustedDomains)); got != tt.want {
				t.Fatalf("getTrustedDomain(%q) = %q, want %q", tt.host, got, tt.want)
			}
		})
	}
}

func TestGetTrustedDomainIgnoresEmptyEntry(t *testing.T) {
	if got := getTrustedDomain([]byte("example.com"), []string{""}); got != nil {
		t.Fatalf("getTrustedDomain matched an empty trusted domain: %q", got)
	}
}

func TestGetDomainAttrExcludesPort(t *testing.T) {
	host := hostWithoutPort([]byte("example.com:8443"))
	if got := getDomainAttr(host); got != "domain=example.com;" {
		t.Fatalf("getDomainAttr(%q) = %q, want %q", host, got, "domain=example.com;")
	}
}

func TestHandleSPOEChallengeRejectsInvalidInput(t *testing.T) {
	validSource := netip.MustParseAddr("192.0.2.1").AsSlice()
	tests := []struct {
		name string
		src  []byte
		host string
	}{
		{name: "invalid source", src: []byte{192, 0, 2}, host: "example.com"},
		{name: "oversized host", src: validSource, host: strings.Repeat("a", hostBufferLength+1)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			message := challengeMessage(t, tt.src, tt.host)
			actions := encoding.NewActionWriter(make([]byte, 2048), 0)
			f := frontend{bh: challengeBerghain()}

			f.HandleSPOEChallenge(context.Background(), actions, message)

			if actions.Off() != 0 {
				t.Fatalf("invalid challenge input produced %d bytes of actions", actions.Off())
			}
		})
	}
}

func challengeMessage(t *testing.T, src []byte, host string) *encoding.Message {
	t.Helper()

	writer := encoding.NewKVWriter(make([]byte, 2048), 0)
	for _, write := range []func() error{
		func() error { return writer.SetInt64("level", 1) },
		func() error { return writer.SetBinary("src", src) },
		func() error { return writer.SetString("host", host) },
		func() error { return writer.SetString("method", http.MethodGet) },
		func() error { return writer.SetBinary("body", nil) },
	} {
		if err := write(); err != nil {
			t.Fatalf("encode challenge message: %v", err)
		}
	}

	return &encoding.Message{KV: encoding.NewKVScanner(writer.Bytes(), 5)}
}

func challengeBerghain() *berghain.Berghain {
	b := berghain.NewBerghain(make([]byte, 32))
	b.Levels = []*berghain.LevelConfig{{
		Countdown: 3,
		Duration:  time.Minute,
		Type:      berghain.ValidationTypeNone,
	}}
	return b
}
