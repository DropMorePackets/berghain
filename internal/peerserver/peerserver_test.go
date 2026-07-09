package peerserver

import (
	"bufio"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/dropmorepackets/haproxy-go/peers"
	"github.com/dropmorepackets/haproxy-go/peers/sticktable"
	"github.com/dropmorepackets/haproxy-go/pkg/encoding"
)

// TestServerPushesEntry connects to the server the way HAProxy does (handshake,
// then read messages) and verifies our hand-marshalled definition + entry decode
// back correctly via the library's receive-side Unmarshal.
func TestServerPushesEntry(t *testing.T) {
	srv := New("berghain_feed", "st_reputation_v4", "st_reputation_v6", time.Hour)
	srv.Set(netip.MustParseAddr("1.2.3.4"), 1)

	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	go srv.Serve(l)

	c, err := net.Dial("tcp", l.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := peers.NewHandshake("berghain_feed").WriteTo(c); err != nil {
		t.Fatal(err)
	}

	br := bufio.NewReader(c)
	if status, err := br.ReadString('\n'); err != nil || status != "200\n" {
		t.Fatalf("handshake status = %q, err = %v", status, err)
	}

	var def *sticktable.Definition
	for {
		class, err := br.ReadByte()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		typ, err := br.ReadByte()
		if err != nil {
			t.Fatalf("read: %v", err)
		}
		if typ < 0x80 {
			continue // control/error message, no payload
		}
		n, err := encoding.ReadVarint(br)
		if err != nil {
			t.Fatal(err)
		}
		payload := make([]byte, n)
		if _, err := io.ReadFull(br, payload); err != nil {
			t.Fatal(err)
		}
		_ = class

		switch peers.StickTableUpdateMessageType(typ) {
		case peers.StickTableUpdateMessageTypeStickTableDefinition:
			var d sticktable.Definition
			if _, err := d.Unmarshal(payload); err != nil {
				t.Fatalf("definition unmarshal: %v", err)
			}
			def = &d
		case peers.StickTableUpdateMessageTypeEntryUpdate:
			if def == nil {
				t.Fatal("entry update before a definition")
			}
			if def.Name != "st_reputation_v4" {
				continue
			}
			e := sticktable.EntryUpdate{StickTable: def, WithLocalUpdateID: true}
			if _, err := e.Unmarshal(payload); err != nil {
				t.Fatalf("entry unmarshal: %v", err)
			}
			if e.Key.String() != "1.2.3.4" {
				continue
			}
			if len(e.Data) != 1 || e.Data[0].String() != "1" {
				t.Fatalf("entry data = %v, want gpt0=1", e.Data)
			}
			return // success
		}
	}
}
