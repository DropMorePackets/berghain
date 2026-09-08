// Package peerserver implements the *sending* side of the HAProxy peers
// protocol: it lets an external process push stick-table entries into a running
// HAProxy without a reload. haproxy-go's peers package only implements the
// receiving side, so the framing/handshake here is built on its exported
// sticktable encoders and message constants.
//
// The server is designed to live in a peers mesh where it is not the only
// peer: it validates handshakes, acknowledges entry updates pushed by the
// other peers (so they consider us synced instead of re-teaching forever),
// and answers resync requests from any number of connections.
//
// It is used to feed individual-IP reputation (bans, Tor exit nodes, ...) into
// HAProxy stick-tables live. Stick-tables key on exact IPs, so CIDR/ASN feeds
// still belong in map files — see internal/reputation.
package peerserver

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/netip"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/dropmorepackets/haproxy-go/peers"
	"github.com/dropmorepackets/haproxy-go/peers/sticktable"
	"github.com/dropmorepackets/haproxy-go/pkg/encoding"
)

// Entry is the desired state for one address.
type Entry struct {
	// Value is the gpt0 tag (e.g. 1=block, 3=challenge); 0 clears the entry.
	Value uint32
	// ExpiresAt bounds the entry's lifetime in HAProxy (sent as a timed
	// update). The zero value means the table's default expiry applies.
	ExpiresAt time.Time
}

// entryState is an Entry plus the update ID it was taught under. Entries keep
// their ID for their whole life so resyncs replay the same IDs that already
// went out and acknowledgements stay unambiguous.
type entryState struct {
	value    uint32
	expireAt time.Time
	id       uint32
}

func (st entryState) expired(now time.Time) bool {
	return !st.expireAt.IsZero() && now.After(st.expireAt)
}

// table holds the entries for one HAProxy stick-table (one address family).
type table struct {
	def     *sticktable.Definition
	keyType sticktable.KeyType

	mu       sync.Mutex
	entries  map[netip.Addr]entryState
	updateID uint32
}

// Server serves one or more reputation stick-tables to connected HAProxy peers.
// The zero value is not usable; use New.
type Server struct {
	localPeer string
	expiry    time.Duration

	v4 *table
	v6 *table

	mu    sync.Mutex
	conns map[*conn]struct{}
}

// New creates a server exposing an IPv4 table and an IPv6 table with the given
// names (which must match the HAProxy `backend` stick-table names) and default
// expiry.
func New(localPeer, v4Name, v6Name string, expiry time.Duration) *Server {
	exp := uint64(expiry.Milliseconds())
	mkTable := func(id uint64, name string, kt sticktable.KeyType, kl uint64) *table {
		return &table{
			keyType: kt,
			entries: make(map[netip.Addr]entryState),
			def: &sticktable.Definition{
				StickTableID: id,
				Name:         name,
				KeyType:      kt,
				KeyLength:    kl,
				DataTypes:    []sticktable.DataTypeDefinition{{DataType: sticktable.DataTypeGPT0}},
				Expiry:       exp,
			},
		}
	}
	return &Server{
		localPeer: localPeer,
		expiry:    expiry,
		v4:        mkTable(1, v4Name, sticktable.KeyTypeIPv4Address, 4),
		v6:        mkTable(2, v6Name, sticktable.KeyTypeIPv6Address, 16),
		conns:     make(map[*conn]struct{}),
	}
}

func (s *Server) tableFor(a netip.Addr) *table {
	if a.Is4() {
		return s.v4
	}
	return s.v6
}

// Set adds or updates a reputation entry and pushes it to all connected peers.
func (s *Server) Set(a netip.Addr, e Entry) {
	a = a.Unmap()
	t := s.tableFor(a)

	t.mu.Lock()
	if cur, ok := t.entries[a]; ok && cur.value == e.Value && cur.expireAt.Equal(e.ExpiresAt) {
		t.mu.Unlock()
		return
	}
	t.updateID++
	st := entryState{value: e.Value, expireAt: e.ExpiresAt, id: t.updateID}
	t.entries[a] = st
	t.mu.Unlock()

	s.broadcast(func(c *conn) { _ = c.sendEntry(t, a, st) })
}

// Delete clears an entry. HAProxy has no peer "delete"; we teach value 0 with
// a bounded expiry so every peer — including ones that reconnect later and
// resync — converges on the entry being gone.
func (s *Server) Delete(a netip.Addr) {
	s.Set(a, Entry{Value: 0, ExpiresAt: time.Now().Add(s.expiry)})
}

// ReplaceAll moves the full entry set to the given desired state (used after a
// feed refresh): new/changed entries are pushed, dropped entries are cleared,
// and locally expired ones are pruned.
func (s *Server) ReplaceAll(values map[netip.Addr]Entry) {
	seen := make(map[netip.Addr]struct{}, len(values))
	for a, e := range values {
		seen[a.Unmap()] = struct{}{}
		s.Set(a, e)
	}

	now := time.Now()
	for _, t := range []*table{s.v4, s.v6} {
		var stale []netip.Addr
		t.mu.Lock()
		for a, st := range t.entries {
			if st.expired(now) {
				delete(t.entries, a)
				continue
			}
			if _, ok := seen[a]; !ok && st.value != 0 {
				stale = append(stale, a)
			}
		}
		t.mu.Unlock()
		for _, a := range stale {
			s.Delete(a)
		}
	}
}

// Get returns the live entry for an address, if any.
func (s *Server) Get(a netip.Addr) (Entry, bool) {
	a = a.Unmap()
	t := s.tableFor(a)
	t.mu.Lock()
	defer t.mu.Unlock()
	st, ok := t.entries[a]
	if !ok || st.value == 0 || st.expired(time.Now()) {
		return Entry{}, false
	}
	return Entry{Value: st.value, ExpiresAt: st.expireAt}, true
}

// Len returns the number of live non-zero entries across both tables.
func (s *Server) Len() int {
	now := time.Now()
	n := 0
	for _, t := range []*table{s.v4, s.v6} {
		t.mu.Lock()
		for _, st := range t.entries {
			if st.value != 0 && !st.expired(now) {
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

	// State of the remote peer's teach stream, needed to acknowledge its
	// updates: entry updates apply to the last announced table definition.
	remoteDef      *sticktable.Definition
	remoteUpdateID uint32
}

func (c *conn) serve() error {
	var h peers.Handshake
	if _, err := h.ReadFrom(c.br); err != nil {
		return fmt.Errorf("handshake: %w", err)
	}

	status := peers.HandshakeStatusHandshakeSucceeded
	switch {
	case h.ProtocolIdentifier != "HAProxyS":
		status = peers.HandshakeStatusProtocolError
	case !strings.HasPrefix(h.Version, "2."):
		status = peers.HandshakeStatusBadVersion
	case h.RemotePeer != c.srv.localPeer:
		// The remote addressed a peer name that is not ours.
		status = peers.HandshakeStatusLocalPeerIdentifierMismatch
	}
	if _, err := c.nc.Write([]byte(fmt.Sprintf("%d\n", status))); err != nil {
		return fmt.Errorf("handshake reply: %w", err)
	}
	if status != peers.HandshakeStatusHandshakeSucceeded {
		return fmt.Errorf("handshake rejected with %d: proto=%q version=%q target=%q from=%q",
			status, h.ProtocolIdentifier, h.Version, h.RemotePeer, h.LocalPeerIdentifier)
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

// readLoop consumes messages from the remote peer. In a mesh the other peers
// teach us *their* stick-table state; we acknowledge those updates so they
// consider this peer synced. Resync requests replay our full state.
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

		// Messages with type >= 128 carry a varint-length-prefixed payload.
		var payload []byte
		if typ >= 0x80 {
			n, err := encoding.ReadVarint(c.br)
			if err != nil {
				return err
			}
			payload = make([]byte, n)
			if _, err := io.ReadFull(c.br, payload); err != nil {
				return err
			}
		}

		switch peers.MessageClass(class) {
		case peers.MessageClassControl:
			if peers.ControlMessageType(typ) == peers.ControlMessageSyncRequest {
				if err := c.fullSync(); err != nil {
					return err
				}
			}
		case peers.MessageClassStickTableUpdates:
			if err := c.handleStickTableMessage(peers.StickTableUpdateMessageType(typ), payload); err != nil {
				return err
			}
		}
	}
}

// handleStickTableMessage tracks the remote teach stream and acknowledges its
// entry updates. Tables whose definition we cannot decode are skipped (their
// updates are consumed without an ack), which keeps unknown data types from
// killing the connection.
func (c *conn) handleStickTableMessage(typ peers.StickTableUpdateMessageType, payload []byte) error {
	switch typ {
	case peers.StickTableUpdateMessageTypeStickTableDefinition:
		var def sticktable.Definition
		if _, err := def.Unmarshal(payload); err != nil {
			slog.Debug("undecodable remote table definition", "error", err)
			c.remoteDef = nil
			return nil
		}
		c.remoteDef = &def
		return nil
	case peers.StickTableUpdateMessageTypeStickTableSwitch:
		// Switches reference an earlier definition by ID; we only track the
		// last one, so treat the stream as unknown until the next definition.
		c.remoteDef = nil
		return nil
	case peers.StickTableUpdateMessageTypeUpdateAcknowledge:
		// Acks for our own updates; resyncs replay stable IDs, so there is
		// nothing to track.
		return nil
	case peers.StickTableUpdateMessageTypeEntryUpdate,
		peers.StickTableUpdateMessageTypeUpdateTimed,
		peers.StickTableUpdateMessageTypeIncrementalEntryUpdate,
		peers.StickTableUpdateMessageTypeIncrementalEntryUpdateTimed:
		if c.remoteDef == nil {
			return nil
		}

		e := sticktable.EntryUpdate{
			StickTable:    c.remoteDef,
			LocalUpdateID: c.remoteUpdateID + 1, // incremental updates imply last+1
		}
		switch typ {
		case peers.StickTableUpdateMessageTypeEntryUpdate:
			e.WithLocalUpdateID = true
		case peers.StickTableUpdateMessageTypeUpdateTimed:
			e.WithLocalUpdateID = true
			e.WithExpiry = true
		case peers.StickTableUpdateMessageTypeIncrementalEntryUpdateTimed:
			e.WithExpiry = true
		}
		if _, err := e.Unmarshal(payload); err != nil {
			slog.Debug("undecodable remote entry update", "error", err)
			return nil
		}
		c.remoteUpdateID = e.LocalUpdateID

		return c.sendAck(c.remoteDef.StickTableID, e.LocalUpdateID)
	default:
		return nil
	}
}

// sendAck acknowledges the remote peer's update so it considers us in sync.
func (c *conn) sendAck(tableID uint64, updateID uint32) error {
	var payload [14]byte
	n, err := encoding.PutVarint(payload[:], tableID)
	if err != nil {
		return err
	}
	binary.BigEndian.PutUint32(payload[n:], updateID)
	return c.sendMessage(peers.MessageClassStickTableUpdates,
		byte(peers.StickTableUpdateMessageTypeUpdateAcknowledge), payload[:n+4])
}

// fullSync sends every table definition and all current entries (in teach
// order), followed by a resync-finished control message.
func (c *conn) fullSync() error {
	now := time.Now()
	for _, t := range []*table{c.srv.v4, c.srv.v6} {
		if err := c.sendDefinition(t); err != nil {
			return err
		}

		type teachEntry struct {
			addr netip.Addr
			st   entryState
		}
		t.mu.Lock()
		snapshot := make([]teachEntry, 0, len(t.entries))
		for a, st := range t.entries {
			if st.expired(now) {
				continue
			}
			snapshot = append(snapshot, teachEntry{addr: a, st: st})
		}
		t.mu.Unlock()

		// Replay in the order the entries were originally taught so the
		// remote's last-seen update ID moves monotonically.
		sort.Slice(snapshot, func(i, j int) bool { return snapshot[i].st.id < snapshot[j].st.id })
		for _, te := range snapshot {
			if err := c.sendEntry(t, te.addr, te.st); err != nil {
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

// sendEntry marshals and sends an entry update (update-id + key + gpt0),
// using the timed variant when the entry carries its own expiry.
func (c *conn) sendEntry(t *table, a netip.Addr, st entryState) error {
	var key sticktable.MapKey
	if t.keyType == sticktable.KeyTypeIPv4Address {
		k := sticktable.IPv4AddressKey(a)
		key = &k
	} else {
		k := sticktable.IPv6AddressKey(a)
		key = &k
	}

	d := sticktable.UnsignedIntegerData(st.value)
	e := sticktable.EntryUpdate{
		StickTable:        t.def,
		WithLocalUpdateID: true,
		LocalUpdateID:     st.id,
		Key:               key,
		Data:              []sticktable.MapData{&d},
	}

	typ := peers.StickTableUpdateMessageTypeEntryUpdate
	if !st.expireAt.IsZero() {
		remaining := max(time.Until(st.expireAt), 0)
		e.WithExpiry = true
		e.Expiry = uint32(remaining.Milliseconds())
		typ = peers.StickTableUpdateMessageTypeUpdateTimed
	}

	buf := make([]byte, 64)
	n, err := e.Marshal(buf)
	if err != nil {
		return err
	}
	return c.sendMessage(peers.MessageClassStickTableUpdates, byte(typ), buf[:n])
}
