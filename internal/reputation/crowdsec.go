package reputation

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"

	"github.com/DropMorePackets/berghain/internal/peerserver"
)

// The decision stream is a small JSON document; limit reads defensively.
const crowdsecMaxStreamLength = 64 << 20

type csDecision struct {
	Scope    string `json:"scope"`
	Value    string `json:"value"`
	Type     string `json:"type"`
	Duration string `json:"duration"`
}

type csStream struct {
	New     []csDecision `json:"new"`
	Deleted []csDecision `json:"deleted"`
}

// crowdsecLoop implements a CrowdSec bouncer: it polls the LAPI decision
// stream and maintains the "crowdsec" reputation source from it. The first
// poll (and the first after any error) passes startup=true so the LAPI
// replays the full active decision set.
func (s *Service) crowdsecLoop(ctx context.Context) {
	slog.InfoContext(ctx, "polling the CrowdSec decision stream",
		"url", s.cfg.CrowdSec.URL, "interval", s.cfg.CrowdSec.Interval.String())

	t := time.NewTicker(s.cfg.CrowdSec.Interval)
	defer t.Stop()

	startup := true
	poll := func() {
		if err := s.pollCrowdSec(ctx, startup); err != nil {
			if ctx.Err() == nil {
				slog.ErrorContext(ctx, "crowdsec decision stream poll failed", "error", err)
			}
			// Resync from scratch once the LAPI is reachable again: deletions
			// that happened while we could not poll are gone from the stream.
			startup = true
			return
		}
		startup = false
	}

	poll()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			poll()
		}
	}
}

func (s *Service) pollCrowdSec(ctx context.Context, startup bool) error {
	u, err := url.Parse(s.cfg.CrowdSec.URL)
	if err != nil {
		return err
	}
	u = u.JoinPath("/v1/decisions/stream")
	q := u.Query()
	q.Set("startup", fmt.Sprintf("%t", startup))
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("X-Api-Key", s.cfg.CrowdSec.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var stream csStream
	if err := json.NewDecoder(io.LimitReader(resp.Body, crowdsecMaxStreamLength)).Decode(&stream); err != nil {
		return err
	}

	// On startup the stream is the full state; otherwise apply the delta on
	// top of what we already know.
	var state map[netip.Addr]peerserver.Entry
	if startup {
		state = make(map[netip.Addr]peerserver.Entry)
	} else {
		state = s.snapshotSource("crowdsec")
		if state == nil {
			state = make(map[netip.Addr]peerserver.Entry)
		}
	}

	now := time.Now()
	var skippedScopes, applied, deleted int
	for _, d := range stream.New {
		a, ok := decisionAddr(d)
		if !ok {
			skippedScopes++
			continue
		}
		e := peerserver.Entry{Value: decisionAction(d.Type)}
		if dur, err := time.ParseDuration(d.Duration); err == nil && dur > 0 {
			e.ExpiresAt = now.Add(dur)
		}
		state[a] = e
		applied++
	}
	for _, d := range stream.Deleted {
		a, ok := decisionAddr(d)
		if !ok {
			continue
		}
		delete(state, a)
		deleted++
	}

	if startup || applied > 0 || deleted > 0 {
		s.setSource("crowdsec", state)
	}
	if skippedScopes > 0 {
		slog.WarnContext(ctx, "skipped non-ip crowdsec decisions",
			"count", skippedScopes, "reason", "stick-tables cannot prefix-match; range decisions need map files")
	}
	return nil
}

func decisionAddr(d csDecision) (netip.Addr, bool) {
	if !strings.EqualFold(d.Scope, "ip") {
		return netip.Addr{}, false
	}
	a, err := netip.ParseAddr(d.Value)
	if err != nil {
		return netip.Addr{}, false
	}
	return a.Unmap(), true
}

// decisionAction maps a CrowdSec remediation type onto our gpt0 actions.
// Unknown remediation types fail closed to a block, matching how CrowdSec
// bouncers treat custom decision types they do not implement.
func decisionAction(typ string) uint32 {
	switch strings.ToLower(typ) {
	case "captcha":
		return actionChallenge
	case "ban":
		return actionBlock
	default:
		return actionBlock
	}
}
