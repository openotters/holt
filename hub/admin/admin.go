// Package admin implements the holt hub's Admin gRPC service over a
// *hub.Registry: list live tunnels, force one closed, and — when a
// Blocker is supplied — block/unblock a peer's credential. Mount the
// returned connect handler behind whatever operator auth you use.
package admin

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	"github.com/openotters/holt"
	holtv1 "github.com/openotters/holt/api/v1"
	"github.com/openotters/holt/hub"
)

// BlockedPeer is one entry in the credential denylist.
type BlockedPeer struct {
	Peer          string
	BlockedAtUnix int64
}

// Blocker is the hub-side credential denylist BlockPeer/UnblockPeer
// drive, and ListBlocked reads. The holt library has no notion of
// credentials, so the application supplies this (e.g. a JWT-subject
// blocklist the auth middleware consults). Optional — without it,
// BlockPeer/UnblockPeer return Unimplemented and ListBlocked is empty.
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
