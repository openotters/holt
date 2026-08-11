// Package admin implements the holt hub's Admin gRPC service over a
// *hub.Registry: list live tunnels, force one closed, and — when a
// Blocker is supplied — ban/unban a peer id. Mount the returned
// connect handler behind whatever operator auth you use.
package admin

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/openotters/holt"
	holtv1 "github.com/openotters/holt/api/v1"
	"github.com/openotters/holt/hub"
)

// BlockedPeer is one entry in the peer-id denylist.
type BlockedPeer struct {
	Peer          string
	BlockedAtUnix int64
}

// Blocker is the hub-side peer-id denylist BlockPeer/UnblockPeer
// drive, and ListBlocked reads. The ban is on the identity, not one
// token. The holt library has no notion of credentials, so the
// application supplies this (e.g. a JWT-subject blocklist the auth
// middleware consults). Optional — without it, BlockPeer/UnblockPeer
// return Unimplemented and ListBlocked is empty.
type Blocker interface {
	Block(peer string)
	Unblock(peer string)
	Blocked() []BlockedPeer
}

// Service implements holtv1connect.AdminHandler against a Registry.
type Service struct {
	registry *hub.Registry
	blocker  Blocker
}

// Option configures a Service.
type Option func(*Service)

// WithBlocker enables BlockPeer/UnblockPeer, wiring them to the given
// credential denylist.
func WithBlocker(b Blocker) Option {
	return func(s *Service) { s.blocker = b }
}

// NewService wires the Admin service to a Registry.
func NewService(registry *hub.Registry, opts ...Option) *Service {
	s := &Service{registry: registry}
	for _, opt := range opts {
		opt(s)
	}

	return s
}

// ListTunnels returns every live tunnel this hub owns.
func (s *Service) ListTunnels(
	_ context.Context, _ *connect.Request[holtv1.ListTunnelsRequest],
) (*connect.Response[holtv1.ListTunnelsResponse], error) {
	live := s.registry.ListTunnels()

	tunnels := make([]*holtv1.TunnelInfo, 0, len(live))
	for _, t := range live {
		tunnels = append(tunnels, &holtv1.TunnelInfo{
			Peer:           t.Peer,
			PeerVersion:    t.PeerVersion,
			AttachedAtUnix: t.AttachedAt.Unix(),
		})
	}

	return connect.NewResponse(&holtv1.ListTunnelsResponse{Tunnels: tunnels}), nil
}

// StopTunnel force-closes a peer's tunnel and reports whether one was
// present.
func (s *Service) StopTunnel(
	_ context.Context, req *connect.Request[holtv1.StopTunnelRequest],
) (*connect.Response[holtv1.StopTunnelResponse], error) {
	reason := req.Msg.GetReason()
	if reason == "" {
		// Terminal, so a killed peer stays down instead of instantly
		// redialing.
		reason = holt.ReasonClosed
	}

	stopped := s.registry.StopTunnel(req.Msg.GetPeer(), reason)

	return connect.NewResponse(&holtv1.StopTunnelResponse{Stopped: stopped}), nil
}

// BlockPeer blocks a peer's credential and closes its live tunnel with
// a token-revoked GoAway.
func (s *Service) BlockPeer(
	_ context.Context, req *connect.Request[holtv1.BlockPeerRequest],
) (*connect.Response[holtv1.BlockPeerResponse], error) {
	if s.blocker == nil {
		return nil, connect.NewError(connect.CodeUnimplemented,
			errors.New("this hub has no credential blocker configured"))
	}

	peer := req.Msg.GetPeer()

	s.blocker.Block(peer)
	stopped := s.registry.StopTunnel(peer, holt.ReasonTokenRevoked)

	return connect.NewResponse(&holtv1.BlockPeerResponse{Stopped: stopped}), nil
}

// UnblockPeer lifts a peer's block.
func (s *Service) UnblockPeer(
	_ context.Context, req *connect.Request[holtv1.UnblockPeerRequest],
) (*connect.Response[holtv1.UnblockPeerResponse], error) {
	if s.blocker == nil {
		return nil, connect.NewError(connect.CodeUnimplemented,
			errors.New("this hub has no credential blocker configured"))
	}

	s.blocker.Unblock(req.Msg.GetPeer())

	return connect.NewResponse(&holtv1.UnblockPeerResponse{}), nil
}

// ListBlocked returns the currently-blocked peers, or an empty list
// when no blocker is configured.
func (s *Service) ListBlocked(
	_ context.Context, _ *connect.Request[holtv1.ListBlockedRequest],
) (*connect.Response[holtv1.ListBlockedResponse], error) {
	resp := &holtv1.ListBlockedResponse{}

	if s.blocker == nil {
		return connect.NewResponse(resp), nil
	}

	for _, b := range s.blocker.Blocked() {
		resp.Peers = append(resp.Peers, &holtv1.BlockedPeer{
			Peer:          b.Peer,
			BlockedAtUnix: b.BlockedAtUnix,
		})
	}

	return connect.NewResponse(resp), nil
}

// WatchTunnels streams the live-tunnel set: a snapshot of the current
// tunnels as ATTACHED events, then live attach/detach transitions. The
// subscription is opened before the snapshot is read, so nothing falls
// in the gap; the overlap can produce a duplicate ATTACHED, which
// clients must treat as idempotent. Returns when the client goes away
// or the registry drops this watcher for being too slow (the client
// resubscribes and gets a fresh snapshot).
func (s *Service) WatchTunnels(
	ctx context.Context, _ *connect.Request[holtv1.WatchTunnelsRequest],
	stream *connect.ServerStream[holtv1.TunnelEvent],
) error {
	events := s.registry.Watch(ctx)

	// Hello marker (KIND_UNSPECIFIED): flushes the response headers so
	// browser clients resolve their fetch immediately and can show the
	// subscription as live even when no tunnel exists yet.
	if err := stream.Send(&holtv1.TunnelEvent{}); err != nil {
		return err
	}

	for _, t := range s.registry.ListTunnels() {
		if err := stream.Send(&holtv1.TunnelEvent{
			Kind: holtv1.TunnelEvent_KIND_ATTACHED,
			Info: &holtv1.TunnelInfo{
				Peer:           t.Peer,
				PeerVersion:    t.PeerVersion,
				AttachedAtUnix: t.AttachedAt.Unix(),
			},
		}); err != nil {
			return err
		}
	}

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				// Dropped for slowness (or registry shutdown); ending the
				// stream tells the client to resubscribe.
				return nil
			}

			out := &holtv1.TunnelEvent{
				Kind:   holtv1.TunnelEvent_KIND_DETACHED,
				Info:   &holtv1.TunnelInfo{Peer: ev.Peer},
				Reason: ev.Reason,
			}
			if ev.Kind == hub.EventAttached {
				out.Kind = holtv1.TunnelEvent_KIND_ATTACHED
				out.Reason = ""
				if t, live := s.registry.Tunnel(ev.Peer); live {
					out.Info.PeerVersion = t.PeerVersion
					out.Info.AttachedAtUnix = t.AttachedAt.Unix()
				}
			}

			if err := stream.Send(out); err != nil {
				return err
			}
		}
	}
}
