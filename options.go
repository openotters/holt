package holt

import (
	"go.uber.org/zap"
)

// SharedOption configures either half: it is accepted by both
// NewServer and NewClient.
type SharedOption interface {
	Option
	ClientOption
}

// sharedOption implements SharedOption over both configs.
type sharedOption struct {
	server func(*Server)
	client func(*Client)
}

func (o sharedOption) applyServer(s *Server) { o.server(s) }
func (o sharedOption) applyClient(c *Client) { o.client(c) }

// WithLogger sets the logger. Default: no logging.
func WithLogger(logger *zap.Logger) SharedOption {
	return sharedOption{
		server: func(s *Server) { s.logger = logger },
		client: func(c *Client) { c.opts.Logger = logger },
	}
}
