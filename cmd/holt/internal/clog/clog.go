// Package clog builds the CLI logger. The default "pretty" format
// renders friendly, human-first lines (charmbracelet/log); "json" is
// the classic structured production log (zap). Both are exposed as a
// *zap.Logger so the hub and dial libraries stay charm-free.
package clog

import (
	"os"
	"sort"
	"time"

	charm "github.com/charmbracelet/log"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

// Format names accepted by New.
const (
	FormatPretty = "pretty"
	FormatJSON   = "json"
)

// New builds the CLI logger for the requested format and level.
func New(format string, level zapcore.Level) (*zap.Logger, error) {
	if format == FormatJSON {
		cfg := zap.NewProductionConfig()
		cfg.Level = zap.NewAtomicLevelAt(level)

		return cfg.Build()
	}

	cl := charm.NewWithOptions(os.Stderr, charm.Options{
		ReportTimestamp: true,
		TimeFormat:      time.Kitchen,
	})

	return zap.New(&charmCore{logger: cl, min: level}), nil
}

// charmCore forwards zap entries to a charm logger, so the libraries
// keep their zap API while humans get readable output.
type charmCore struct {
	logger *charm.Logger
	min    zapcore.Level
	fields []zapcore.Field
}

func (c *charmCore) Enabled(lvl zapcore.Level) bool { return lvl >= c.min }

func (c *charmCore) With(fields []zapcore.Field) zapcore.Core {
	clone := &charmCore{logger: c.logger, min: c.min}
	clone.fields = append(append(clone.fields, c.fields...), fields...)

	return clone
}

func (c *charmCore) Check(entry zapcore.Entry, checked *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if c.Enabled(entry.Level) {
		return checked.AddCore(entry, c)
	}

	return checked
}

func (c *charmCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	enc := zapcore.NewMapObjectEncoder()
	for _, f := range c.fields {
		f.AddTo(enc)
	}

	for _, f := range fields {
		f.AddTo(enc)
	}

	keys := make([]string, 0, len(enc.Fields))
	for k := range enc.Fields {
		keys = append(keys, k)
	}

	sort.Strings(keys)

	keyvals := make([]any, 0, len(keys)*2)
	for _, k := range keys {
		keyvals = append(keyvals, k, enc.Fields[k])
	}

	logger := c.logger
	if entry.LoggerName != "" {
		logger = logger.WithPrefix(entry.LoggerName)
	}

	// Fatal/panic levels render as errors here; zap still exits or
	// panics right after Write, per its own semantics.
	switch {
	case entry.Level >= zapcore.ErrorLevel:
		logger.Error(entry.Message, keyvals...)
	case entry.Level == zapcore.WarnLevel:
		logger.Warn(entry.Message, keyvals...)
	case entry.Level == zapcore.DebugLevel:
		logger.Debug(entry.Message, keyvals...)
	default:
		logger.Info(entry.Message, keyvals...)
	}

	return nil
}

func (c *charmCore) Sync() error { return nil }
