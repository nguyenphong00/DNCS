package requestmeta

import "context"

type contextKey struct{}

// Metadata contains identifiers that are safe to propagate to logs and audit
// rows. IPHash is keyed before it enters the context so the raw client address
// is never exposed to downstream observability code.
type Metadata struct {
	RequestID     string
	CorrelationID string
	IPHash        string
}

func WithContext(ctx context.Context, metadata Metadata) context.Context {
	return context.WithValue(ctx, contextKey{}, metadata)
}

func FromContext(ctx context.Context) (Metadata, bool) {
	metadata, ok := ctx.Value(contextKey{}).(Metadata)
	return metadata, ok
}
