package peerserver

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/dropmorepackets/haproxy-go/peers"
	"github.com/dropmorepackets/haproxy-go/peers/sticktable"
	"github.com/dropmorepackets/haproxy-go/pkg/encoding"
)

func startServer(t *testing.T, srv *Server) net.Addr {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go srv.Serve(l)
	return l.Addr()
}

func dialPeer(t *testing.T, addr net.Addr, targetPeer string) (net.Conn, *bufio.Reader) {
	t.Helper()
	c, err := net.Dial("tcp", addr.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { c.Close() })
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))

	if _, err := peers.NewHandshake(targetPeer).WriteTo(c); err != nil {
		t.Fatal(err)
	}
	return c, bufio.NewReader(c)
}

type frame struct {
	class   byte
	typ     byte
	payload []byte
}

func readFrame(t *testing.T, br *bufio.Reader) frame {
	t.Helper()
	class, err := br.ReadByte()
	if err != nil {
		t.Fatalf("read class: %v", err)
	}
	typ, err := br.ReadByte()
	if err != nil {
		t.Fatalf("read type: %v", err)
	}
	var payload []byte
	if typ >= 0x80 {
		n, err := encoding.ReadVarint(br)
		if err != nil {
			t.Fatal(err)
		}
		payload = make([]byte, n)
		if _, err := io.ReadFull(br, payload); err != nil {
			t.Fatal(err)
		}
	}
	return frame{class: class, typ: typ, payload: payload}
}

// TestServerPushesEntry connects to the server the way HAProxy does (handshake,
// then read messages) and verifies our hand-marshalled definition + entry decode
// back correctly via the library's receive-side Unmarshal.
func TestServerPushesEntry(t *testing.T) {
	srv := New("berghain_feed", "st_reputation_v4", "st_reputation_v6", time.Hour)
	srv.Set(netip.MustParseAddr("1.2.3.4"), Entry{Value: 1})

	addr := startServer(t, srv)
	_, br := dialPeer(t, addr, "berghain_feed")

	if status, err := br.ReadString('\n'); err != nil || status != "200\n" {
		t.Fatalf("handshake status = %q, err = %v", status, err)
	}

	var def *sticktable.Definition
	for {
		f := readFrame(t, br)
		if peers.MessageClass(f.class) != peers.MessageClassStickTableUpdates {
			continue
		}

		switch peers.StickTableUpdateMessageType(f.typ) {
		case peers.StickTableUpdateMessageTypeStickTableDefinition:
			var d sticktable.Definition
			if _, err := d.Unmarshal(f.payload); err != nil {
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
			if _, err := e.Unmarshal(f.payload); err != nil {
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

// TestServerPushesTimedEntry verifies entries with their own expiry go out as
// timed updates carrying the remaining lifetime.
func TestServerPushesTimedEntry(t *testing.T) {
	srv := New("berghain_feed", "st_reputation_v4", "st_reputation_v6", time.Hour)
	srv.Set(netip.MustParseAddr("1.2.3.4"), Entry{Value: 3, ExpiresAt: time.Now().Add(4 * time.Hour)})

	addr := startServer(t, srv)
	_, br := dialPeer(t, addr, "berghain_feed")

	if status, err := br.ReadString('\n'); err != nil || status != "200\n" {
		t.Fatalf("handshake status = %q, err = %v", status, err)
	}

	var def *sticktable.Definition
	for {
		f := readFrame(t, br)

		switch peers.StickTableUpdateMessageType(f.typ) {
		case peers.StickTableUpdateMessageTypeStickTableDefinition:
			var d sticktable.Definition
			if _, err := d.Unmarshal(f.payload); err != nil {
				t.Fatalf("definition unmarshal: %v", err)
			}
			def = &d
		case peers.StickTableUpdateMessageTypeEntryUpdate:
			t.Fatal("expected a timed update, got a plain entry update")
		case peers.StickTableUpdateMessageTypeUpdateTimed:
			if def == nil || def.Name != "st_reputation_v4" {
				continue
			}
			e := sticktable.EntryUpdate{StickTable: def, WithLocalUpdateID: true, WithExpiry: true}
			if _, err := e.Unmarshal(f.payload); err != nil {
				t.Fatalf("timed entry unmarshal: %v", err)
			}
			remaining := time.Duration(e.Expiry) * time.Millisecond
			if remaining <= 3*time.Hour || remaining > 4*time.Hour {
				t.Fatalf("timed entry expiry = %v, want just under 4h", remaining)
			}
			return // success
		}
	}
}

// TestHandshakeRejectsWrongPeer verifies that a handshake addressed to a
// different local peer name is answered with 503 and the connection dropped.
func TestHandshakeRejectsWrongPeer(t *testing.T) {
	srv := New("berghain_feed", "st_reputation_v4", "st_reputation_v6", time.Hour)
	addr := startServer(t, srv)
	_, br := dialPeer(t, addr, "some_other_peer")

	if status, err := br.ReadString('\n'); err != nil || status != "503\n" {
		t.Fatalf("handshake status = %q, err = %v, want 503", status, err)
	}
	if _, err := br.ReadByte(); err != io.EOF {
		t.Fatalf("expected connection close after reject, got err = %v", err)
	}
}

// TestServerAcksRemoteUpdates plays the other-peer role: teach the server a
// table and an entry update, and expect an UpdateAcknowledge with our table ID
// and update ID back. This is what keeps a multi-peer mesh from re-teaching us
// on every reconnect.
func TestServerAcksRemoteUpdates(t *testing.T) {
	srv := New("berghain_feed", "st_reputation_v4", "st_reputation_v6", time.Hour)
	addr := startServer(t, srv)
	c, br := dialPeer(t, addr, "berghain_feed")

	if status, err := br.ReadString('\n'); err != nil || status != "200\n" {
		t.Fatalf("handshake status = %q, err = %v", status, err)
	}

	send := func(typ peers.StickTableUpdateMessageType, payload []byte) {
		t.Helper()
		var lenbuf [10]byte
		n, err := encoding.PutVarint(lenbuf[:], uint64(len(payload)))
		if err != nil {
			t.Fatal(err)
		}
		msg := append([]byte{byte(peers.MessageClassStickTableUpdates), byte(typ)}, lenbuf[:n]...)
		if _, err := c.Write(append(msg, payload...)); err != nil {
			t.Fatal(err)
		}
	}

	def := &sticktable.Definition{
		StickTableID: 7,
		Name:         "st_src",
		KeyType:      sticktable.KeyTypeIPv4Address,
		KeyLength:    4,
		DataTypes:    []sticktable.DataTypeDefinition{{DataType: sticktable.DataTypeGPT0}},
		Expiry:       60000,
	}
	buf := make([]byte, 256)
	n, err := def.Marshal(buf)
	if err != nil {
		t.Fatal(err)
	}
	send(peers.StickTableUpdateMessageTypeStickTableDefinition, buf[:n])

	key := sticktable.IPv4AddressKey(netip.MustParseAddr("9.9.9.9"))
	data := sticktable.UnsignedIntegerData(1)
	e := sticktable.EntryUpdate{
		StickTable:        def,
		WithLocalUpdateID: true,
		LocalUpdateID:     42,
		Key:               &key,
		Data:              []sticktable.MapData{&data},
	}
	n, err = e.Marshal(buf)
	if err != nil {
		t.Fatal(err)
	}
	send(peers.StickTableUpdateMessageTypeEntryUpdate, buf[:n])

	for {
		f := readFrame(t, br)
		if peers.MessageClass(f.class) != peers.MessageClassStickTableUpdates ||
			peers.StickTableUpdateMessageType(f.typ) != peers.StickTableUpdateMessageTypeUpdateAcknowledge {
			continue
		}
		tableID, n, err := encoding.Varint(f.payload)
		if err != nil {
			t.Fatal(err)
		}
		if tableID != 7 {
			t.Fatalf("acked table ID = %d, want 7", tableID)
		}
		if got := binary.BigEndian.Uint32(f.payload[n:]); got != 42 {
			t.Fatalf("acked update ID = %d, want 42", got)
		}
		return // success
	}
}

// TestDeleteConvergesOnResync verifies a deleted entry is taught as a zeroed,
// expiring entry to fresh connections, so peers that missed the live delete
// still converge.
func TestDeleteConvergesOnResync(t *testing.T) {
	srv := New("berghain_feed", "st_reputation_v4", "st_reputation_v6", time.Hour)
	srv.Set(netip.MustParseAddr("1.2.3.4"), Entry{Value: 1})
	srv.Delete(netip.MustParseAddr("1.2.3.4"))

	if srv.Len() != 0 {
		t.Fatalf("Len() = %d after delete, want 0", srv.Len())
	}

	addr := startServer(t, srv)
	_, br := dialPeer(t, addr, "berghain_feed")

	if status, err := br.ReadString('\n'); err != nil || status != "200\n" {
		t.Fatalf("handshake status = %q, err = %v", status, err)
	}

	var def *sticktable.Definition
	for {
		f := readFrame(t, br)

		switch peers.StickTableUpdateMessageType(f.typ) {
		case peers.StickTableUpdateMessageTypeStickTableDefinition:
			var d sticktable.Definition
			if _, err := d.Unmarshal(f.payload); err != nil {
				t.Fatal(err)
			}
			def = &d
		case peers.StickTableUpdateMessageTypeUpdateTimed:
			if def == nil || def.Name != "st_reputation_v4" {
				continue
			}
			e := sticktable.EntryUpdate{StickTable: def, WithLocalUpdateID: true, WithExpiry: true}
			if _, err := e.Unmarshal(f.payload); err != nil {
				t.Fatal(err)
			}
			if e.Key.String() != "1.2.3.4" {
				continue
			}
			if e.Data[0].String() != "0" {
				t.Fatalf("resynced deleted entry has gpt0=%s, want 0", e.Data[0])
			}
			return // success
		}
	}
}
