// Package holt implements a reverse HTTP tunnel over a WebSocket
// carrying a bidirectional protobuf frame stream: a peer that can
// only dial OUT attaches to a hub, then serves an ordinary
// http.Handler back through the attached stream. The hub gets an
// http.RoundTripper per peer and a presence signal for free. The
// carrier is a WebSocket so the tunnel passes through CDN public
// hostnames, access proxies, and HTTP/1.1-only edges that cannot
// proxy gRPC.
//
// The module ships both halves:
//
//   - hub:  the server side — accept Attach streams, keep a registry
//     of live tunnels, dial "through" any peer with a RoundTripper.
//   - dial: the client side — a persistent attach loop that serves a
//     handler over the tunnel and redials with backoff.
//
// This package holds what both halves share: the frame-level
// net.Conn adapter, the handshake, and the GoAway vocabulary.
package holt

import (
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	holtv1 "github.com/openotters/holt/api/v1"
)

// ProtocolVersion is the tunnel framing version spoken by this
// module, offered in Hello and confirmed in Welcome.
const ProtocolVersion = 1

// MaxDataFrame caps one TunnelFrame data payload — keeps inner
// HTTP/2 stream interleaving granular and each WebSocket message
// small enough for any intermediary.
const MaxDataFrame = 32 * 1024

// Well-known GoAway reasons. Superseded, credential revocation, and
// deliberate stop are terminal for the peer (no redial); everything
// else means "redial with backoff".
const (
	ReasonSuperseded   = "superseded"
	ReasonTokenRevoked = "token-revoked"
	ReasonShuttingDown = "shutting-down"
	ReasonPeerStopping = "peer-stopping"
	// ReasonClosed is an operator-initiated close (e.g. an admin
	// "kill"): terminal, so the peer stops redialing until it is
	// restarted or re-enrolled.
	ReasonClosed = "closed"
)

// TerminalReason reports whether a GoAway reason tells the peer to
// stop redialing.
func TerminalReason(reason string) bool {
	switch reason {
	case ReasonSuperseded, ReasonTokenRevoked, ReasonPeerStopping, ReasonClosed:
		return true
	default:
		return false
	}
}

// FrameStream is the frame-level view of an attached tunnel: one
// TunnelFrame per Send/Recv. NewWSStream adapts a WebSocket
// connection to it; tests may substitute in-memory implementations.
type FrameStream interface {
	Send(*holtv1.TunnelFrame) error
	Recv() (*holtv1.TunnelFrame, error)
}

// GoAwayError carries the far end's GoAway reason.
type GoAwayError struct{ Reason string }

func (e GoAwayError) Error() string { return "holt: go-away: " + e.Reason }

// GoAwayReason returns the GoAway reason carried by err, or "" when
// err is not a GoAway.
func GoAwayReason(err error) string {
	var goAway GoAwayError
	if errors.As(err, &goAway) {
		return goAway.Reason
	}

	return ""
}

// Conn adapts an attached FrameStream to a net.Conn carrying raw
// bytes as TunnelFrame data frames. Read is single-reader — the
// HTTP/2 session on top owns it — and writes are serialised
// internally because frame streams do not allow concurrent sends.
//
// Close seals the adapter: it waits out any in-flight write, then
// makes every later I/O attempt fail locally with net.ErrClosed
// instead of reaching the stream. Hub handlers rely on this — the
// HTTP/2 transport's background goroutines (ping, resets) outlive
// the handler, and a late write must never reach a finished
// handler's socket.
type Conn struct {
	stream  frameIO
	closeFn func() error
	local   string
	remote  string

	readBuf []byte

	writeMu sync.Mutex
	closed  bool

	errMu   sync.Mutex
	lastErr error // first terminal read error, GoAway included
}

// frameIO is FrameStream, unexported to keep the constructor the
// only way in.
type frameIO interface {
	Send(*holtv1.TunnelFrame) error
	Recv() (*holtv1.TunnelFrame, error)
}

// ConnOption configures a Conn.
type ConnOption func(*Conn)

// WithCloseFunc runs fn once when the Conn is closed — the dial side
// passes the stream's CloseSend so the hub sees a clean EOF.
func WithCloseFunc(fn func() error) ConnOption {
	return func(c *Conn) { c.closeFn = fn }
}

// WithSides labels LocalAddr/RemoteAddr for logs ("hub"/"peer").
func WithSides(local, remote string) ConnOption {
	return func(c *Conn) { c.local, c.remote = local, remote }
}

// NewConn wraps an attached (post-handshake) stream.
func NewConn(stream FrameStream, opts ...ConnOption) *Conn {
	c := &Conn{stream: stream, local: "local", remote: "remote"}
	for _, opt := range opts {
		opt(c)
	}

	return c
}

// Read returns buffered tunnel bytes, receiving the next data frame
// when the buffer is empty. A GoAway ends the stream with io.EOF
// after recording the reason (see LastError).
func (c *Conn) Read(p []byte) (int, error) {
	for len(c.readBuf) == 0 {
		if c.isClosed() {
			return 0, net.ErrClosed
		}

		frame, err := c.stream.Recv()
		if err != nil {
			c.recordErr(err)

			return 0, err
		}

		switch kind := frame.GetKind().(type) {
		case *holtv1.TunnelFrame_Data:
			c.readBuf = kind.Data
		case *holtv1.TunnelFrame_GoAway:
			c.recordErr(GoAwayError{Reason: kind.GoAway.GetReason()})

			return 0, io.EOF
		default:
			frameErr := fmt.Errorf("holt: unexpected frame %T after handshake", kind)
			c.recordErr(frameErr)

			return 0, frameErr
		}
	}

	n := copy(p, c.readBuf)
	c.readBuf = c.readBuf[n:]

	return n, nil
}

// Write chunks p into data frames of at most MaxDataFrame bytes.
func (c *Conn) Write(p []byte) (int, error) {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closed {
		return 0, net.ErrClosed
	}

	written := 0
	for len(p) > 0 {
		chunk := p
		if len(chunk) > MaxDataFrame {
			chunk = chunk[:MaxDataFrame]
		}

		frame := &holtv1.TunnelFrame{
			Kind: &holtv1.TunnelFrame_Data{Data: chunk},
		}
		if err := c.stream.Send(frame); err != nil {
			return written, err
		}

		written += len(chunk)
		p = p[len(chunk):]
	}

	return written, nil
}

// SendGoAway emits a GoAway frame under the write mutex so it can
// never interleave with a data frame.
func (c *Conn) SendGoAway(reason string) error {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	if c.closed {
		return net.ErrClosed
	}

	return c.stream.Send(&holtv1.TunnelFrame{
		Kind: &holtv1.TunnelFrame_GoAway{GoAway: &holtv1.GoAway{Reason: reason}},
	})
}

// Close seals the adapter (see the type comment) and runs the
// configured close func once.
func (c *Conn) Close() error {
	c.writeMu.Lock()

	if c.closed {
		c.writeMu.Unlock()

		return nil
	}

	c.closed = true
	fn := c.closeFn
	c.writeMu.Unlock()

	if fn != nil {
		return fn()
	}

	return nil
}

func (c *Conn) isClosed() bool {
	c.writeMu.Lock()
	defer c.writeMu.Unlock()

	return c.closed
}

// LastError returns the first terminal read error; a GoAway from the
// far end surfaces here with its reason intact.
func (c *Conn) LastError() error {
	c.errMu.Lock()
	defer c.errMu.Unlock()

	return c.lastErr
}

func (c *Conn) recordErr(err error) {
	c.errMu.Lock()
	defer c.errMu.Unlock()

	if c.lastErr == nil {
		c.lastErr = err
	}
}

// tunnelAddr is the placeholder net.Addr — the tunnel has no socket
// address of its own.
type tunnelAddr struct{ side string }

func (a tunnelAddr) Network() string { return "tunnel" }
func (a tunnelAddr) String() string  { return a.side }

func (c *Conn) LocalAddr() net.Addr  { return tunnelAddr{side: c.local} }
func (c *Conn) RemoteAddr() net.Addr { return tunnelAddr{side: c.remote} }

// Deadlines are accepted but not enforced: liveness comes from the
// HTTP/2 session's PING on top and the stream's own context.
func (c *Conn) SetDeadline(time.Time) error      { return nil }
func (c *Conn) SetReadDeadline(time.Time) error  { return nil }
func (c *Conn) SetWriteDeadline(time.Time) error { return nil }

var _ net.Conn = (*Conn)(nil)

// ClientHandshake sends Hello and requires Welcome as the first
// reply. Called by the dial side on a fresh stream.
func ClientHandshake(stream FrameStream, peerVersion string) error {
	hello := &holtv1.TunnelFrame{
		Kind: &holtv1.TunnelFrame_Hello{Hello: &holtv1.Hello{
			ProtocolVersion: ProtocolVersion,
			PeerVersion:     peerVersion,
		}},
	}
	if err := stream.Send(hello); err != nil {
		return fmt.Errorf("holt: send hello: %w", err)
	}

	frame, err := stream.Recv()
	if err != nil {
		return fmt.Errorf("holt: awaiting welcome: %w", err)
	}

	switch kind := frame.GetKind().(type) {
	case *holtv1.TunnelFrame_Welcome:
		if got := kind.Welcome.GetProtocolVersion(); got != ProtocolVersion {
			return fmt.Errorf("holt: hub selected unsupported protocol version %d", got)
		}

		return nil
	case *holtv1.TunnelFrame_GoAway:
		return GoAwayError{Reason: kind.GoAway.GetReason()}
	default:
		return fmt.Errorf("holt: expected welcome, got %T", kind)
	}
}

// ServerHandshake requires Hello as the first frame and answers
// Welcome. Called by the hub on a fresh stream. Returns the peer's
// Hello for observability.
func ServerHandshake(stream FrameStream) (*holtv1.Hello, error) {
	frame, err := stream.Recv()
	if err != nil {
		return nil, err
	}

	hello := frame.GetHello()
	if hello == nil {
		return nil, fmt.Errorf("holt: first frame must be hello, got %T", frame.GetKind())
	}

	if hello.GetProtocolVersion() != ProtocolVersion {
		return nil, fmt.Errorf("holt: unsupported tunnel protocol version %d", hello.GetProtocolVersion())
	}

	welcome := &holtv1.TunnelFrame{
		Kind: &holtv1.TunnelFrame_Welcome{Welcome: &holtv1.Welcome{
			ProtocolVersion: ProtocolVersion,
		}},
	}
	if sendErr := stream.Send(welcome); sendErr != nil {
		return nil, fmt.Errorf("holt: send welcome: %w", sendErr)
	}

	return hello, nil
}
