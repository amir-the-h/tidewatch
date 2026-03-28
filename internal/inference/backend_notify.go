package inference

import "context"

// backendNotifyKey is the context key for backend notification callbacks.
type backendNotifyKey struct{}

// WithBackendNotify returns a context carrying a backend notification callback.
// The callback is called by OllamaPipeline when a backend transitions state:
//   - "waiting" — before blocking in acquire()
//   - b.name    — once a backend is acquired
//
// Non-Ollama pipelines never call this callback; the Backend field stays "".
func WithBackendNotify(ctx context.Context, fn func(url string)) context.Context {
	return context.WithValue(ctx, backendNotifyKey{}, fn)
}

// notifyBackend calls the registered callback if one is present in ctx.
// Safe to call even when no callback is set.
func notifyBackend(ctx context.Context, url string) {
	if fn, ok := ctx.Value(backendNotifyKey{}).(func(string)); ok {
		fn(url)
	}
}
