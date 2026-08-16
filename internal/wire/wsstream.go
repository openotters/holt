package wire

import (
	"context"
	"fmt"

	"github.com/coder/websocket"
	"google.golang.org/protobuf/proto"

	holtv1 "github.com/openotters/holt/api/v1"
)

// maxWSMessage bounds one WebSocket message: a TunnelFrame is at most
// MaxDataFrame of payload plus a small protobuf envelope, so 64 KiB
// leaves comfortable headroom while still rejecting garbage early.
const maxWSMessage = 64 * 1024

// wsStream adapts a WebSocket connection to a FrameStream: one binary
// message carries one marshalled TunnelFrame. This is the tunnel's
// wire carrier — WebSockets pass through proxies, CDNs, and access
// layers (Cloudflare public hostnames included) that cannot carry
// gRPC.
type wsStream struct {
	// The FrameStream shape has no context parameter (it mirrors a
	// stream that owns its lifetime), so the connection's lifetime
	// context is captured at construction instead of passed per call.
	ctx context.Context //nolint:containedctx // FrameStream methods take no ctx; see above.
	c   *websocket.Conn
}

// NewWSStream wraps an open WebSocket connection as a FrameStream.
// ctx bounds every send and receive — pass the context that governs
// the connection's lifetime (the attach context on the peer, the
// request context on the hub).
func NewWSStream(ctx context.Context, c *websocket.Conn) FrameStream {
	c.SetReadLimit(maxWSMessage)

	return &wsStream{ctx: ctx, c: c}
}

func (s *wsStream) Send(f *holtv1.TunnelFrame) error {
	raw, err := proto.Marshal(f)
	if err != nil {
		return fmt.Errorf("holt: marshal frame: %w", err)
	}

	return s.c.Write(s.ctx, websocket.MessageBinary, raw)
}

func (s *wsStream) Recv() (*holtv1.TunnelFrame, error) {
	kind, raw, err := s.c.Read(s.ctx)
	if err != nil {
		return nil, err
	}

	if kind != websocket.MessageBinary {
		return nil, fmt.Errorf("holt: unexpected %v websocket message, want binary", kind)
	}

	frame := &holtv1.TunnelFrame{}
	if unmarshalErr := proto.Unmarshal(raw, frame); unmarshalErr != nil {
		return nil, fmt.Errorf("holt: unmarshal frame: %w", unmarshalErr)
	}

	return frame, nil
}
