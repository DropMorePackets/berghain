// Package peerserver implements the *sending* side of the HAProxy peers
// protocol: it lets an external process push stick-table entries into a running
// HAProxy without a reload. haproxy-go's peers package only implements the
// receiving side, so the framing/handshake here is built on its exported
// sticktable encoders and message constants.
//
// It is used to feed individual-IP reputation (bans, Tor exit nodes, ...) into
// HAProxy stick-tables live. Stick-tables key on exact IPs, so CIDR/ASN feeds
// still belong in map files — see cmd/feedupdater.
package peerserver

import (
	"bufio"
	"fmt"
	"log/slog"
	"net"
	"net/netip"
	"sync"
	"time"

	"github.com/dropmorepackets/haproxy-go/peers"
	"github.com/dropmorepackets/haproxy-go/peers/sticktable"
	"github.com/dropmorepackets/haproxy-go/pkg/encoding"
)

// table holds the entries for one HAProxy stick-table (one address family).
type table struct {
	def     *sticktable.Definition
	keyType sticktable.KeyType
	keyLen  uint64

	mu       sync.Mutex
	entries  map[netip.Addr]uint32 // key -> gpt0 value
	updateID uint32
}

// Server serves one or more reputation stick-tables to connected HAProxy peers.
// The zero value is not usable; use New.
type Server struct {
	localPeer string

	v4 *table
	v6 *table

	mu    sync.Mutex
	conns map[*conn]struct{}
}

// New creates a server exposing an IPv4 table and an IPv6 table with the given
// names (which must match the HAProxy `backend` stick-table names) and expiry.
func New(localPeer, v4Name, v6Name string, expiry time.Duration) *Server {
	exp := uint64(expiry.Milliseconds())
	mkTable := func(name string, kt sticktable.KeyType, kl uint64) *table {
		return &table{
			keyType: kt,
			keyLen:  kl,
			entries: make(map[netip.Addr]uint32),
			def: &sticktable.Definition{
				Name:      name,
				KeyType:   kt,
				KeyLength: kl,
				DataTypes: []sticktable.DataTypeDefinition{{DataType: sticktable.DataTypeGPT0}},
				Expiry:    exp,
			},
		}
	}
	return &Server{
		localPeer: localPeer,
		v4:        mkTable(v4Name, sticktable.KeyTypeIPv4Address, 4),
		v6:        mkTable(v6Name, sticktable.KeyTypeIPv6Address, 16),
		conns:     make(map[*conn]struct{}),
	}
}

func (s *Server) tableFor(a netip.Addr) *table {
	if a.Is4() {
		return s.v4
	}
	return s.v6
}

// Set adds or updates a reputation entry (gpt0 value, e.g. 1=block, 2/3=level)
// and pushes it to all connected peers.
func (s *Server) Set(a netip.Addr, value uint32) {
	t := s.tableFor(a.Unmap())
	a = a.Unmap()

	t.mu.Lock()
	if cur, ok := t.entries[a]; ok && cur == value {
		t.mu.Unlock()
		return
	}
	t.updateID++
	id := t.updateID
	t.entries[a] = value
	t.mu.Unlock()

	s.broadcast(func(c *conn) { _ = c.sendEntry(t, a, id, value) })
}

// Delete removes an entry. HAProxy has no peer "delete"; we signal removal by
// setting the tag to 0, which callers treat as "no action".
func (s *Server) Delete(a netip.Addr) {
	s.Set(a, 0)
}

// ReplaceAll atomically replaces the full entry set (used after a feed refresh):
// new/changed entries are pushed and dropped entries are zeroed.
func (s *Server) ReplaceAll(values map[netip.Addr]uint32) {
	seen := make(map[netip.Addr]struct{}, len(values))
	for a, v := range values {
		seen[a.Unmap()] = struct{}{}
		s.Set(a, v)
	}
	for _, t := range []*table{s.v4, s.v6} {
		t.mu.Lock()
		var stale []netip.Addr
		for a, v := range t.entries {
			if _, ok := seen[a]; !ok && v != 0 {
				stale = append(stale, a)
			}
		}
		t.mu.Unlock()
		for _, a := range stale {
			s.Set(a, 0)
		}
	}
}

// Len returns the number of non-zero entries across both tables.
func (s *Server) Len() int {
	n := 0
	for _, t := range []*table{s.v4, s.v6} {
		t.mu.Lock()
		for _, v := range t.entries {
			if v != 0 {
				n++
			}
		}
		t.mu.Unlock()
	}
	return n
}

func (s *Server) broadcast(fn func(*conn)) {
	s.mu.Lock()
	conns := make([]*conn, 0, len(s.conns))
	for c := range s.conns {
		conns = append(conns, c)
	}
	s.mu.Unlock()
	for _, c := range conns {
		fn(c)
	}
}

// Serve accepts HAProxy peer connections until the listener is closed.
func (s *Server) Serve(l net.Listener) error {
	for {
		nc, err := l.Accept()
		if err != nil {
			return err
		}
		c := &conn{nc: nc, br: bufio.NewReader(nc), srv: s}
		s.mu.Lock()
		s.conns[c] = struct{}{}
		s.mu.Unlock()
		go func() {
			if err := c.serve(); err != nil {
				slog.Debug("peer connection closed", "error", err)
			}
			s.mu.Lock()
			delete(s.conns, c)
			s.mu.Unlock()
			_ = nc.Close()
		}()
	}
}

type conn struct {
	nc  net.Conn
	br  *bufio.Reader
	srv *Server

	wmu sync.Mutex // serialises all writes on this connection
}

func (c *conn) serve() error {
	var h peers.Handshake
	if _, err := h.ReadFrom(c.br); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}
	if _, err := c.nc.Write([]byte(fmt.Sprintf("%d\n", peers.HandshakeStatusHandshakeSucceeded))); err != nil {
		return fmt.Errorf("handshake reply: %w", err)
	}
	slog.Debug("peer connected", "remote", h.LocalPeerIdentifier)

	// Proactively announce tables and push the current state.
	if err := c.fullSync(); err != nil {
		return err
	}

	// Heartbeat so HAProxy does not consider us dead (5s timeout).
	stop := make(chan struct{})
	defer close(stop)
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-stop:
				return
			case <-t.C:
				c.wmu.Lock()
				_, err := c.nc.Write([]byte{byte(peers.MessageClassControl), byte(peers.ControlMessageHeartbeat)})
				c.wmu.Unlock()
				if err != nil {
					return
				}
			}
		}
	}()

	return c.readLoop()
}

// readLoop consumes messages, keeping framing in sync (payload messages carry a
// varint length), and re-syncs on a resync request.
func (c *conn) readLoop() error {
	for {
		class, err := c.br.ReadByte()
		if err != nil {
			return err
		}
		typ, err := c.br.ReadByte()
		if err != nil {
			return err
		}

		// Messages with type >= 128 have a varint-length-prefixed payload.
		if typ >= 0x80 {
			n, err := encoding.ReadVarint(c.br)
			if err != nil {
				return err
			}
			if _, err := c.br.Discard(int(n)); err != nil {
				return err
			}
			continue
		}

		if peers.MessageClass(class) == peers.MessageClassControl &&
			peers.ControlMessageType(typ) == peers.ControlMessageSyncRequest {
			if err := c.fullSync(); err != nil {
				return err
			}
		}
	}
}

// fullSync sends every table definition and all current entries, followed by a
// resync-finished control message.
func (c *conn) fullSync() error {
	for _, t := range []*table{c.srv.v4, c.srv.v6} {
		if err := c.sendDefinition(t); err != nil {
			return err
		}
		t.mu.Lock()
		snapshot := make(map[netip.Addr]uint32, len(t.entries))
		for a, v := range t.entries {
			snapshot[a] = v
		}
		t.mu.Unlock()
		for a, v := range snapshot {
			if v == 0 {
				continue
			}
			t.mu.Lock()
			t.updateID++
			id := t.updateID
			t.mu.Unlock()
			if err := c.sendEntry(t, a, id, v); err != nil {
				return err
			}
		}
	}
	c.wmu.Lock()
	_, err := c.nc.Write([]byte{byte(peers.MessageClassControl), byte(peers.ControlMessageSyncFinished)})
	c.wmu.Unlock()
	return err
}

func (c *conn) sendMessage(class peers.MessageClass, typ byte, payload []byte) error {
	var lenbuf [10]byte
	ln, err := encoding.PutVarint(lenbuf[:], uint64(len(payload)))
	if err != nil {
		return err
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if _, err := c.nc.Write([]byte{byte(class), typ}); err != nil {
		return err
	}
	if _, err := c.nc.Write(lenbuf[:ln]); err != nil {
		return err
	}
	_, err = c.nc.Write(payload)
	return err
}

func (c *conn) sendDefinition(t *table) error {
	buf := make([]byte, 256)
	n, err := t.def.Marshal(buf)
	if err != nil {
		return err
	}
	return c.sendMessage(peers.MessageClassStickTableUpdates,
		byte(peers.StickTableUpdateMessageTypeStickTableDefinition), buf[:n])
}

// sendEntry marshals and sends an entry update (update-id + key + gpt0).
func (c *conn) sendEntry(t *table, a netip.Addr, updateID uint32, value uint32) error {
	var key sticktable.MapKey
	if t.keyType == sticktable.KeyTypeIPv4Address {
		k := sticktable.IPv4AddressKey(a)
		key = &k
	} else {
		k := sticktable.IPv6AddressKey(a)
		key = &k
	}

	d := sticktable.UnsignedIntegerData(value)
	e := sticktable.EntryUpdate{
		StickTable:        t.def,
		WithLocalUpdateID: true,
		LocalUpdateID:     updateID,
		Key:               key,
		Data:              []sticktable.MapData{&d},
	}

	buf := make([]byte, 64)
	n, err := e.Marshal(buf)
	if err != nil {
		return err
	}
	return c.sendMessage(peers.MessageClassStickTableUpdates,
		byte(peers.StickTableUpdateMessageTypeEntryUpdate), buf[:n])
}
