// Package forward owns the gateway's hot-path: take an IR request,
// translate it through a Driver, dispatch via a shared HTTP client,
// and stream or copy the response back to the client.
//
// This is the function that becomes the ForwarderGuard once the Guard
// pipeline lands. Keeping it as a plain function for the MVP makes it
// easier to wire end-to-end and refactor into a Guard later without
// changing the underlying logic.
package forward

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// Forwarder dispatches an IR request through a Driver and forwards
// the response. It owns the shared HTTP client (connection pool,
// timeouts) that all upstream calls use.
type Forwarder struct {
	client *http.Client
}

// New returns a Forwarder using the given client. A nil client falls
// back to a tuned default suitable for high-concurrency LLM
// forwarding: large idle pool, HTTP/2 enabled, no overall request
// timeout (streaming responses may run for tens of seconds).
func New(client *http.Client) *Forwarder {
	if client == nil {
		client = defaultClient()
	}
	return &Forwarder{client: client}
}

func defaultClient() *http.Client {
	return &http.Client{
		// No Timeout — streaming responses may run for tens of seconds.
		Transport: &http.Transport{
			MaxIdleConnsPerHost:   100,
			MaxConnsPerHost:       200,
			MaxIdleConns:          1000,
			IdleConnTimeout:       90 * time.Second,
			ForceAttemptHTTP2:     true,
			DisableCompression:    true,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: 60 * time.Second,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 30 * time.Second,
			}).DialContext,
		},
	}
}

// Forward builds an outbound request through the driver, dispatches
// it, and either streams (SSE) or copies (single response) the body
// back to w. Returns the upstream HTTP status code and any error that
// occurred.
//
// When Forward returns nil, the response has been fully delivered to
// the client. When it returns a non-nil error, the response may be
// partially written (in the streaming case) — callers should not try
// to send their own error envelope after Forward starts streaming.
func (f *Forwarder) Forward(
	ctx context.Context,
	w http.ResponseWriter,
	req *ir.UnifiedRequest,
	driver provider.Driver,
	account *provider.Account,
) (int, error) {
	if req == nil {
		return 0, errors.New("forward: nil request")
	}
	if driver == nil {
		return 0, errors.New("forward: nil driver")
	}

	upstreamReq, err := driver.BuildRequest(ctx, req, account)
	if err != nil {
		return 0, fmt.Errorf("build request: %w", err)
	}

	resp, err := f.client.Do(upstreamReq)
	if err != nil {
		return 0, fmt.Errorf("upstream call: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return resp.StatusCode, forwardErrorBody(w, resp)
	}
	if req.Stream {
		return resp.StatusCode, forwardSSE(w, resp)
	}
	return resp.StatusCode, forwardBody(w, resp)
}

// forwardErrorBody copies an upstream error response verbatim. The
// status code is preserved so the client sees Anthropic's exact reply.
func forwardErrorBody(w http.ResponseWriter, resp *http.Response) error {
	copySafeHeaders(w, resp)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	_, err := io.Copy(w, resp.Body)
	return err
}

// forwardBody copies a successful non-streaming response.
func forwardBody(w http.ResponseWriter, resp *http.Response) error {
	copySafeHeaders(w, resp)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	_, err := io.Copy(w, resp.Body)
	return err
}

// forwardSSE pipes upstream SSE chunks to the client, flushing at
// every event boundary so tokens reach the client without buffering.
func forwardSSE(w http.ResponseWriter, resp *http.Response) error {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return errors.New("streaming requires http.Flusher")
	}

	copySafeHeaders(w, resp)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.WriteHeader(resp.StatusCode)

	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, werr := w.Write(line); werr != nil {
				// Client disconnected — stop draining the upstream.
				return fmt.Errorf("write client: %w", werr)
			}
			// Flush at SSE event boundary (blank line) to push tokens.
			if len(bytes.TrimRight(line, "\r\n")) == 0 {
				flusher.Flush()
			}
		}
		if errors.Is(err, io.EOF) {
			flusher.Flush()
			return nil
		}
		if err != nil {
			return fmt.Errorf("read upstream: %w", err)
		}
	}
}

// copySafeHeaders forwards selected response headers from upstream to
// the client. We deliberately allowlist rather than passing everything:
// upstream may include Set-Cookie or other headers we should not echo.
func copySafeHeaders(dst http.ResponseWriter, src *http.Response) {
	for _, k := range []string{
		"x-request-id",
		"anthropic-ratelimit-requests-remaining",
		"anthropic-ratelimit-requests-reset",
		"anthropic-ratelimit-tokens-remaining",
		"anthropic-ratelimit-tokens-reset",
		"retry-after",
	} {
		if v := src.Header.Get(k); v != "" {
			dst.Header().Set(k, v)
		}
	}
}
