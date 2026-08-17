// Package admin implements the holt hub's Admin gRPC service over a
// *registry.Registry: list live tunnels, force one closed, and — when a
// Blocker is supplied — ban/unban a peer id. Mount the returned
// connect handler behind whatever operator auth you use.
package admin

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	holtv1 "github.com/openotters/holt/api/v1"

	"github.com/openotters/holt/pkg/blocklist"
	"github.com/openotters/holt/pkg/registry"
	"github.com/openotters/holt/pkg/reqlog"
)

// BlockedPeer is one entry in the peer-id denylist.
type BlockedPeer = blocklist.BlockedPeer

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

// HubInfo is the static hub metadata Info reports alongside the live
// counts. The library has no notion of these (build, listener
// addresses), so the application supplies them.
type HubInfo struct {
	Version       string
	Commit        string
	AdvertiseAddr string
	ProxyAddr     string
	RouteHeader   string
	MetricsAddr   string // empty when metrics are off
	ExternalURL   string
	TokenTTL      time.Duration
	// ProxyRouting is how the proxy picks the target peer: "header",
	// "subdomain", or "both". ProxyDomain is the base domain the
	// subdomain strategies match (<peer>.<domain>), empty when
	// subdomain routing is off.
	ProxyRouting string
	ProxyDomain  string
}

// Service implements holtv1connect.AdminHandler against a Registry.
type Service struct {
	registry *registry.Registry
	blocker  Blocker
	info     HubInfo
	requests *reqlog.Broker
}

// Option configures a Service.
type Option func(*Service)

// WithBlocker enables BlockPeer/UnblockPeer, wiring them to the given
// credential denylist.
func WithBlocker(b Blocker) Option {
	return func(s *Service) { s.blocker = b }
}

// WithInfo supplies the static metadata Info reports.
func WithInfo(i HubInfo) Option {
	return func(s *Service) { s.info = i }
}

// WithRequests enables WatchRequests, streaming from the broker the
// proxy publishes into. Without it the RPC is Unimplemented.
func WithRequests(b *reqlog.Broker) Option {
	return func(s *Service) { s.requests = b }
}

// NewService wires the Admin service to a Registry.
func NewService(registry *registry.Registry, opts ...Option) *Service {
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
			TunnelType:     t.Type.String(),
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
		reason = registry.ReasonClosed
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
	stopped := s.registry.StopTunnel(peer, registry.ReasonTokenRevoked)

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
				TunnelType:     t.Type.String(),
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
			if ev.Kind == registry.EventAttached {
				out.Kind = holtv1.TunnelEvent_KIND_ATTACHED
				out.Reason = ""
				if t, live := s.registry.Tunnel(ev.Peer); live {
					out.Info.PeerVersion = t.PeerVersion
					out.Info.AttachedAtUnix = t.AttachedAt.Unix()
					out.Info.TunnelType = t.Type.String()
				}
			}

			if err := stream.Send(out); err != nil {
				return err
			}
		}
	}
}

// WatchRequests streams what the proxy carried, as each response
// completes: the few events the broker still holds, then live ones.
// A request naming a peer gets only that peer's, filtered here.
// Without a Requests broker the hub carries no such view, which is
// Unimplemented rather than a stream that never sends. Returns when
// the client goes away.
func (s *Service) WatchRequests(
	ctx context.Context, req *connect.Request[holtv1.WatchRequestsRequest],
	stream *connect.ServerStream[holtv1.RequestEvent],
) error {
	if s.requests == nil {
		return connect.NewError(connect.CodeUnimplemented,
			errors.New("this hub does not report proxied requests"))
	}

	// Filtering here, not in the client: a console watching one peer
	// should not be sent a fleet's traffic to throw away.
	peer := req.Msg.GetPeer()
	events := s.requests.Watch(ctx)

	for {
		select {
		case <-ctx.Done():
			return nil
		case ev, ok := <-events:
			if !ok {
				return nil
			}

			if peer != "" && ev.Peer != peer {
				continue
			}

			if err := stream.Send(&holtv1.RequestEvent{
				Peer:            ev.Peer,
				Method:          ev.Method,
				Path:            ev.Path,
				Status:          int32(ev.Status), //nolint:gosec // an HTTP status is three digits
				DurationUs:      ev.Duration.Microseconds(),
				AtUnixMillis:    ev.At.UnixMilli(),
				Query:           ev.Query,
				Host:            ev.Host,
				Proto:           ev.Proto,
				RemoteAddr:      ev.RemoteAddr,
				UserAgent:       ev.UserAgent,
				RequestBytes:    ev.RequestBytes,
				ResponseBytes:   ev.ResponseBytes,
				RequestHeaders:  ev.RequestHeaders,
				ResponseHeaders: ev.ResponseHeaders,
				RequestBody:     asProtoBody(ev.RequestBody),
				ResponseBody:    asProtoBody(ev.ResponseBody),
			}); err != nil {
				return err
			}
		}
	}
}

// asProtoBody carries a captured body over the wire, or nothing when
// there was nothing to carry.
func asProtoBody(b reqlog.Body) *holtv1.Body {
	if b.Size == 0 && b.Skipped == "" {
		return nil
	}

	return &holtv1.Body{
		Content:   b.Content,
		Size:      b.Size,
		Truncated: b.Truncated,
		Skipped:   b.Skipped,
	}
}

// Info reports a snapshot of the hub: the static metadata supplied via
// WithInfo, plus the live tunnel and blocked counts.
func (s *Service) Info(
	_ context.Context, _ *connect.Request[holtv1.InfoRequest],
) (*connect.Response[holtv1.InfoResponse], error) {
	var blocked int64
	if s.blocker != nil {
		blocked = int64(len(s.blocker.Blocked()))
	}

	return connect.NewResponse(&holtv1.InfoResponse{
		Version:         s.info.Version,
		Commit:          s.info.Commit,
		Tunnels:         int64(s.registry.CountTunnels()),
		Blocked:         blocked,
		AdvertiseAddr:   s.info.AdvertiseAddr,
		ProxyAddr:       s.info.ProxyAddr,
		RouteHeader:     s.info.RouteHeader,
		MetricsAddr:     s.info.MetricsAddr,
		ExternalUrl:     s.info.ExternalURL,
		TokenTtlSeconds: int64(s.info.TokenTTL / time.Second),
		ProxyRouting:    s.info.ProxyRouting,
		ProxyDomain:     s.info.ProxyDomain,
	}), nil
}
