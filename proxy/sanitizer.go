package proxy

import (
	"context"
	"log/slog"
	"strings"
)

// SanitizingHandler wraps another handler and sanitizes sensitive information
type SanitizingHandler struct {
	wrapped slog.Handler
}

// NewSanitizingHandler creates a new sanitizing handler
func NewSanitizingHandler(wrapped slog.Handler) *SanitizingHandler {
	return &SanitizingHandler{wrapped: wrapped}
}

// Enabled implements slog.Handler
func (h *SanitizingHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.wrapped.Enabled(ctx, level)
}

// Handle implements slog.Handler
func (h *SanitizingHandler) Handle(ctx context.Context, r slog.Record) error {
	// Sanitize the message
	r.Message = sanitizeString(r.Message)

	// Create a new record with sanitized attributes
	sanitized := slog.NewRecord(r.Time, r.Level, r.Message, r.PC)

	// Sanitize each attribute
	r.Attrs(func(a slog.Attr) bool {
		sanitized.AddAttrs(sanitizeAttr(a))
		return true
	})

	return h.wrapped.Handle(ctx, sanitized)
}

// WithAttrs implements slog.Handler
func (h *SanitizingHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	sanitized := make([]slog.Attr, len(attrs))
	for i, attr := range attrs {
		sanitized[i] = sanitizeAttr(attr)
	}
	return &SanitizingHandler{wrapped: h.wrapped.WithAttrs(sanitized)}
}

// WithGroup implements slog.Handler
func (h *SanitizingHandler) WithGroup(name string) slog.Handler {
	return &SanitizingHandler{wrapped: h.wrapped.WithGroup(name)}
}

// sanitizeAttr sanitizes a single attribute
func sanitizeAttr(a slog.Attr) slog.Attr {
	switch a.Value.Kind() {
	case slog.KindString:
		return slog.Attr{
			Key:   a.Key,
			Value: slog.StringValue(sanitizeString(a.Value.String())),
		}
	case slog.KindGroup:
		// Recursively sanitize group attributes
		group := a.Value.Group()
		sanitized := make([]any, len(group))
		for i, attr := range group {
			sanitized[i] = sanitizeAttr(attr)
		}
		return slog.Group(a.Key, sanitized...)
	default:
		return a
	}
}

// sanitizeString removes potentially sensitive information from strings
func sanitizeString(s string) string {
	// Remove full paths that might contain usernames after /home/
	if strings.Contains(s, "/home/") {
		parts := strings.Split(s, "/home/")
		for i := 1; i < len(parts); i++ {
			subParts := strings.SplitN(parts[i], "/", 2)
			if len(subParts) > 1 {
				// Replace username with <user>
				parts[i] = "<user>/" + subParts[1]
			}
		}
		s = strings.Join(parts, "/home/")
	}

	// Remove potential SSH key fingerprints (they look like SHA256:...)
	if strings.Contains(s, "SHA256:") {
		// Find and replace the fingerprint part
		idx := strings.Index(s, "SHA256:")
		if idx >= 0 {
			endIdx := idx + 7 // Length of "SHA256:"
			// Find the end of the fingerprint (usually ends with space or end of string)
			for endIdx < len(s) && s[endIdx] != ' ' && s[endIdx] != '\n' {
				endIdx++
			}
			s = s[:idx+7] + "<redacted>" + s[endIdx:]
		}
	}

	return s
}
