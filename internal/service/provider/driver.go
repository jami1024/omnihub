package provider

import (
	"context"
	"io"
	"net/http"

	"github.com/jami1024/omnihub/internal/ir"
)

// Driver is the contract every upstream LLM integration implements.
//
// A driver does three things: build outbound HTTP requests, decode
// non-streaming responses, and decode streaming responses. Everything
// else (HTTP client tuning, retry, circuit breaker, billing) lives
// outside the driver so it can be reused across all drivers.
type Driver interface {
	// Name returns the provider identifier used to look this driver
	// up from the Registry. It must match Account.Provider.
	Name() string

	// Capabilities advertises which features this driver supports.
	// Drivers must return a stable value across calls.
	Capabilities() Capabilities

	// BuildRequest translates an IR request into a fully-formed
	// outbound HTTP request, including target URL, body, and
	// authentication headers / signatures. The Forwarder then sends
	// the request through the shared HTTP client.
	//
	// The returned request must carry a non-nil Body when applicable
	// and have its Context derived from ctx.
	BuildRequest(ctx context.Context, req *ir.UnifiedRequest, account *Account) (*http.Request, error)

	// ParseResponse converts a successful non-streaming upstream
	// response into IR. Callers must guarantee resp.StatusCode is in
	// the 2xx range; error-status handling lives in the Forwarder.
	//
	// ParseResponse must read resp.Body to completion but must not
	// close it; the caller (Forwarder) owns the body lifecycle.
	ParseResponse(resp *http.Response) (*ir.UnifiedResponse, error)

	// DecodeStream returns an iterator over IR chunks for a streaming
	// upstream response. The iterator takes ownership of body and
	// closes it when Close is called or when Next returns io.EOF.
	DecodeStream(body io.ReadCloser) StreamIter
}

// Capabilities advertises optional features a driver supports. Guards
// and the Resolver use these flags to filter eligible accounts for a
// given request (e.g. skip a driver that does not implement vision
// for a request that includes image content).
type Capabilities struct {
	Chat       bool
	Streaming  bool
	Tools      bool
	Vision     bool
	Thinking   bool
	Embeddings bool
}

// StreamIter yields IR chunks produced by a driver's stream decoder.
//
// Usage:
//
//	it := driver.DecodeStream(resp.Body)
//	defer it.Close()
//	for {
//	    chunk, err := it.Next()
//	    if errors.Is(err, io.EOF) { break }
//	    if err != nil { return err }
//	    // forward chunk to client
//	}
type StreamIter interface {
	// Next returns the next chunk. It returns io.EOF when the stream
	// is fully consumed. Other errors indicate decode or transport
	// failure and abort the iteration.
	Next() (*ir.UnifiedChunk, error)

	// Close releases the underlying body and any decoder state. It
	// is safe to call Close more than once; subsequent calls return
	// the first error.
	Close() error
}
