package wire_test

import (
	"bytes"
	"errors"
	"io"
	"testing"

	holtv1 "github.com/openotters/holt/api/v1"
	"github.com/openotters/holt/internal/wire"
)

// fakeStream is an in-memory FrameStream.
type fakeStream struct {
	sent   []*holtv1.TunnelFrame
	queued []*holtv1.TunnelFrame
}

func (f *fakeStream) Send(frame *holtv1.TunnelFrame) error {
	f.sent = append(f.sent, frame)

	return nil
}

func (f *fakeStream) Recv() (*holtv1.TunnelFrame, error) {
	if len(f.queued) == 0 {
		return nil, io.EOF
	}

	frame := f.queued[0]
	f.queued = f.queued[1:]

	return frame, nil
}

func data(b []byte) *holtv1.TunnelFrame {
	return &holtv1.TunnelFrame{Kind: &holtv1.TunnelFrame_Data{Data: b}}
}

func TestConn_ReadDrainsDataFrames(t *testing.T) {
	t.Parallel()

	s := &fakeStream{queued: []*holtv1.TunnelFrame{data([]byte("hello ")), data([]byte("holt"))}}
	c := wire.NewConn(s)

	got := make([]byte, 0, 13)
	buf := make([]byte, 4)

	for {
		n, err := c.Read(buf)
		got = append(got, buf[:n]...)

		if err != nil {
			if !errors.Is(err, io.EOF) {
				t.Fatalf("read: %v", err)
			}

			break
		}
	}

	if string(got) != "hello holt" {
		t.Fatalf("read %q", got)
	}
}

func TestConn_WriteChunks(t *testing.T) {
	t.Parallel()

	s := &fakeStream{}
	c := wire.NewConn(s)

	payload := bytes.Repeat([]byte("x"), wire.MaxDataFrame+100)
	if _, err := c.Write(payload); err != nil {
		t.Fatalf("write: %v", err)
	}

	if len(s.sent) != 2 {
		t.Fatalf("sent %d frames, want 2", len(s.sent))
	}
	if len(s.sent[0].GetData()) != wire.MaxDataFrame || len(s.sent[1].GetData()) != 100 {
		t.Fatalf("bad chunk split: %d, %d", len(s.sent[0].GetData()), len(s.sent[1].GetData()))
	}
}

func TestConn_GoAwaySurfacesReason(t *testing.T) {
	t.Parallel()

	s := &fakeStream{queued: []*holtv1.TunnelFrame{
		{Kind: &holtv1.TunnelFrame_GoAway{GoAway: &holtv1.GoAway{Reason: "superseded"}}},
	}}
	c := wire.NewConn(s)

	if _, err := c.Read(make([]byte, 8)); !errors.Is(err, io.EOF) {
		t.Fatalf("read = %v, want EOF", err)
	}
	if r := wire.GoAwayReason(c.LastError()); r != "superseded" {
		t.Fatalf("reason = %q", r)
	}
}

func TestConn_SealedAfterClose(t *testing.T) {
	t.Parallel()

	c := wire.NewConn(&fakeStream{})
	_ = c.Close()

	if _, err := c.Write([]byte("x")); !errors.Is(err, io.ErrClosedPipe) && err == nil {
		t.Fatal("write after close should fail")
	}
	if _, err := c.Read(make([]byte, 4)); err == nil {
		t.Fatal("read after close should fail")
	}
}

func TestHandshake_RoundTrip(t *testing.T) {
	t.Parallel()

	// Server sees the client's Hello and answers Welcome; client
	// accepts it. Drive both sides over a shared queue.
	toServer := &fakeStream{}
	// Client sends Hello into toServer.sent; feed it to the server.
	if err := clientSendHello(toServer); err != nil {
		t.Fatal(err)
	}

	server := &fakeStream{queued: toServer.sent}
	hello, err := wire.ServerHandshake(server)
	if err != nil {
		t.Fatalf("server handshake: %v", err)
	}
	if hello.GetProtocolVersion() != wire.ProtocolVersion {
		t.Fatalf("hello version = %d", hello.GetProtocolVersion())
	}

	// Client consumes the server's Welcome.
	client := &fakeStream{queued: server.sent}
	// Re-send hello (ClientHandshake sends then receives); a fresh
	// stream that already has Welcome queued lets us assert accept.
	if hsErr := wire.ClientHandshake(client, "test", holtv1.TunnelType_TUNNEL_TYPE_HTTP); hsErr != nil {
		t.Fatalf("client handshake: %v", hsErr)
	}
}

func TestTerminalReason(t *testing.T) {
	t.Parallel()

	for _, r := range []string{wire.ReasonSuperseded, wire.ReasonTokenRevoked, wire.ReasonPeerStopping} {
		if !wire.TerminalReason(r) {
			t.Errorf("%q should be terminal", r)
		}
	}
	for _, r := range []string{wire.ReasonShuttingDown, "connection-lost", "whatever"} {
		if wire.TerminalReason(r) {
			t.Errorf("%q should not be terminal", r)
		}
	}
}

// clientSendHello sends just the Hello frame (the first half of
// ClientHandshake) so the server side can be tested in isolation.
func clientSendHello(s wire.FrameStream) error {
	return s.Send(&holtv1.TunnelFrame{
		Kind: &holtv1.TunnelFrame_Hello{Hello: &holtv1.Hello{ProtocolVersion: wire.ProtocolVersion}},
	})
}
