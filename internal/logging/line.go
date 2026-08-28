package logging

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"
)

const lineTimeLayout = "2006-01-02 15:04:05.000"

type lineHandler struct {
	writer io.Writer
	opts   slog.HandlerOptions
	attrs  []slog.Attr
	groups []string
	mu     *sync.Mutex
}

func newLineHandler(writer io.Writer, opts *slog.HandlerOptions) slog.Handler {
	options := slog.HandlerOptions{}
	if opts != nil {
		options = *opts
	}
	if options.Level == nil {
		options.Level = slog.LevelInfo
	}
	return &lineHandler{writer: writer, opts: options, mu: &sync.Mutex{}}
}

func (h *lineHandler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.opts.Level.Level()
}

func (h *lineHandler) Handle(_ context.Context, record slog.Record) error {
	timestamp := record.Time
	if timestamp.IsZero() {
		timestamp = time.Now()
	}
	var builder strings.Builder
	builder.Grow(128 + len(record.Message))
	builder.WriteString(timestamp.Local().Format(lineTimeLayout))
	builder.WriteString(" [")
	builder.WriteString(levelLabel(record.Level))
	builder.WriteString("] - ")
	builder.WriteString(record.Message)
	for _, attr := range h.attrs {
		appendAttr(&builder, h.groups, attr)
	}
	record.Attrs(func(attr slog.Attr) bool {
		appendAttr(&builder, h.groups, attr)
		return true
	})
	builder.WriteByte('\n')
	h.mu.Lock()
	defer h.mu.Unlock()
	_, err := h.writer.Write([]byte(builder.String()))
	return err
}

func (h *lineHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	cloned := *h
	cloned.attrs = append(append([]slog.Attr(nil), h.attrs...), attrs...)
	return &cloned
}

func (h *lineHandler) WithGroup(name string) slog.Handler {
	if name == "" {
		return h
	}
	cloned := *h
	cloned.groups = append(append([]string(nil), h.groups...), name)
	return &cloned
}

func levelLabel(level slog.Level) string {
	switch {
	case level < slog.LevelInfo:
		return "DEBUG"
	case level < slog.LevelWarn:
		return "INFO"
	case level < slog.LevelError:
		return "WARN"
	default:
		return "ERROR"
	}
}

func appendAttr(builder *strings.Builder, groups []string, attr slog.Attr) {
	attr.Value = attr.Value.Resolve()
	if attr.Equal(slog.Attr{}) {
		return
	}
	if attr.Value.Kind() == slog.KindGroup {
		next := groups
		if attr.Key != "" {
			next = append(append([]string(nil), groups...), attr.Key)
		}
		for _, nested := range attr.Value.Group() {
			appendAttr(builder, next, nested)
		}
		return
	}
	builder.WriteByte(' ')
	for _, group := range groups {
		builder.WriteString(group)
		builder.WriteByte('.')
	}
	builder.WriteString(attr.Key)
	builder.WriteByte('=')
	builder.WriteString(formatValue(attr.Value))
}

func formatValue(value slog.Value) string {
	switch value.Kind() {
	case slog.KindString:
		return quoteIfNeeded(value.String())
	case slog.KindInt64:
		return strconv.FormatInt(value.Int64(), 10)
	case slog.KindUint64:
		return strconv.FormatUint(value.Uint64(), 10)
	case slog.KindFloat64:
		return strconv.FormatFloat(value.Float64(), 'g', -1, 64)
	case slog.KindBool:
		return strconv.FormatBool(value.Bool())
	case slog.KindDuration:
		return value.Duration().String()
	case slog.KindTime:
		return value.Time().Local().Format(lineTimeLayout)
	default:
		return quoteIfNeeded(fmt.Sprint(value.Any()))
	}
}

func quoteIfNeeded(value string) string {
	if value == "" || strings.ContainsAny(value, " \t\n=\"") {
		return strconv.Quote(value)
	}
	return value
}
