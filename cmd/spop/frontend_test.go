package main

import "testing"

func TestHostWithoutPort(t *testing.T) {
	tests := []struct {
		in   string
		want string
	}{
		{"example.com", "example.com"},
		{"example.com:8443", "example.com"},
		{"foo.example.com:8080", "foo.example.com"},
		{"[::1]:8080", "[::1]"},
		{"[2001:db8::1]:443", "[2001:db8::1]"},
		{"[::1]", "[::1]"},
		{"", ""},
	}
	for _, tt := range tests {
		if got := string(hostWithoutPort([]byte(tt.in))); got != tt.want {
			t.Errorf("hostWithoutPort(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestGetTrustedDomainWithPort(t *testing.T) {
	td := []string{"foo.example.com"}
	// A host carrying a port must still match its trusted domain once the port
	// is stripped (issue #46).
	host := hostWithoutPort([]byte("sub.foo.example.com:8443"))
	if got := getTrustedDomain(host, td); string(got) != "foo.example.com" {
		t.Errorf("getTrustedDomain = %q, want foo.example.com", got)
	}
}

func TestGetDomainAttrNoPort(t *testing.T) {
	// The cookie Domain attribute must never contain a port.
	host := hostWithoutPort([]byte("example.com:8443"))
	if got := getDomainAttr(host); got != "domain=example.com;" {
		t.Errorf("getDomainAttr = %q, want domain=example.com;", got)
	}
}
