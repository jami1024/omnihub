// Package forward owns the gateway's hot-path: take an IR request,
// translate it through a Driver, dispatch via a shared HTTP client,
// and stream or copy the response back to the client.
//
// The two-phase API (Dispatch + WriteResponse) exists so the handler
// can retry on a different account when the upstream returns a
// retriable error (5xx, 429) without leaking partial bytes to the
// client. Dispatch sends the request and surfaces the response
// status; the caller decides whether to commit or roll the dice
// again on the next account.
//
// Forward is kept as a convenience wrapper that does both phases for
// callers that do not need retry semantics.
package forward

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/provider"
	"github.com/jami1024/omnihub/internal/service/usage"
)

// Result reports what the Forwarder learned from one upstream call.
// All fields are populated when WriteResponse returns nil error; some
// are populated even on error (e.g. StatusCode for a passthrough).
type Result struct {
	// StatusCode is the upstream HTTP status (passed through to client).
	StatusCode int

	// Usage holds extracted token / model / id fields from the upstream
	// response. Zero value when nothing could be parsed (e.g. error
	// responses, non-Anthropic shapes).
	Usage usage.Usage

	// TTFB is the time from sending the upstream request to receiving
	// the first body byte. Only meaningful for streaming responses;
	// zero for non-streaming.
	TTFB time.Duration

	// ErrorBody is the upstream response body captured when StatusCode
	// is >= 400. Truncated to maxCapturedErrorBodyBytes so we don't
	// blow up the WriteBuffer rows when an upstream misbehaves.
	ErrorBody []byte
}

// maxCapturedErrorBodyBytes caps how much of an upstream error body
// we keep for diagnostics. Anthropic-style error JSON is well under
// 1 KiB; the extra room is a safety margin for verbose upstreams.
const maxCapturedErrorBodyBytes = 8 << 10

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

// Dispatch sends an upstream request and returns the response. The
// caller owns resp.Body and must Close it (or pass it to
// WriteResponse which closes it).
//
// requestSentAt is the wall-clock instant the upstream request left
// the local process; WriteResponse uses it to compute TTFB for
// streaming responses.
func (f *Forwarder) Dispatch(
	ctx context.Context,
	req *ir.UnifiedRequest,
	driver provider.Driver,
	account *provider.Account,
) (resp *http.Response, requestSentAt time.Time, err error) {
	if req == nil {
		return nil, time.Time{}, errors.New("forward: nil request")
	}
	if driver == nil {
		return nil, time.Time{}, errors.New("forward: nil driver")
	}

	upstreamReq, err := driver.BuildRequest(ctx, req, account)
	if err != nil {
		return nil, time.Time{}, fmt.Errorf("build request: %w", err)
	}

	// Belt-and-suspenders: drivers should never set forwarding
	// headers, but if a future driver inherits one from the inbound
	// request it would leak the client's IP to the upstream. Drop
	// the common set explicitly so the upstream always sees the
	// gateway's outbound IP as the originating address.
	for _, h := range []string{
		"X-Forwarded-For",
		"X-Forwarded-Host",
		"X-Forwarded-Proto",
		"X-Real-IP",
		"Forwarded",
		"CF-Connecting-IP",
		"True-Client-IP",
	} {
		upstreamReq.Header.Del(h)
	}

	// Force identity transfer encoding regardless of what the driver
	// or the inbound client requested. A gzip / br compressed
	// response interferes with streaming SSE: Go's http transport
	// transparently decompresses the body, but compression buffers
	// chunks and ruins the per-event flush cadence. claude-code-hub
	// learned the same lesson; we apply it gateway-wide.
	upstreamReq.Header.Set("Accept-Encoding", "identity")

	requestSentAt = time.Now()
	resp, err = f.client.Do(upstreamReq)
	if err != nil {
		return nil, requestSentAt, fmt.Errorf("upstream call: %w", err)
	}
	return resp, requestSentAt, nil
}

// WriteResponse pipes resp to w. For streaming requests an SSE
// sniffer reads each line in parallel with passing it to the client
// to extract token usage. For non-streaming requests the body is
// read fully, usage is parsed, then the bytes are written to the
// client. Always closes resp.Body.
func (f *Forwarder) WriteResponse(
	w http.ResponseWriter,
	resp *http.Response,
	req *ir.UnifiedRequest,
	requestSentAt time.Time,
) (Result, error) {
	defer resp.Body.Close()

	result := Result{StatusCode: resp.StatusCode}

	if resp.StatusCode >= 400 {
		body, err := forwardErrorBody(w, resp, req)
		result.ErrorBody = body
		return result, err
	}
	if req.Stream {
		ttfb, u, err := forwardSSE(w, resp, requestSentAt)
		result.TTFB = ttfb
		result.Usage = u
		return result, err
	}
	u, err := forwardBody(w, resp)
	result.Usage = u
	return result, err
}

// Forward is the convenience one-shot path: Dispatch then
// WriteResponse with no retry semantics. Existing call sites use
// this; the new handler-level retry loop calls Dispatch and
// WriteResponse directly.
func (f *Forwarder) Forward(
	ctx context.Context,
	w http.ResponseWriter,
	req *ir.UnifiedRequest,
	driver provider.Driver,
	account *provider.Account,
) (Result, error) {
	resp, sentAt, err := f.Dispatch(ctx, req, driver, account)
	if err != nil {
		return Result{}, err
	}
	return f.WriteResponse(w, resp, req, sentAt)
}

// forwardErrorBody copies an upstream error response to the client
// and captures up to maxCapturedErrorBodyBytes of it for diagnostics.
// When the upstream error message references a specific tool index
// (e.g. "tools.0.custom.input_schema: ..."), the body is augmented
// with the offending tool's name so the client sees which tool is
// at fault rather than just an opaque index.
func forwardErrorBody(w http.ResponseWriter, resp *http.Response, req *ir.UnifiedRequest) ([]byte, error) {
	copySafeHeaders(w, resp)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}

	// Error bodies from Anthropic are well under 1 MiB; the cap is a
	// guard against a misbehaving upstream rather than a performance
	// constraint. We need the full body to inspect / rewrite, so
	// buffering is unavoidable on this path.
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	augmented := augmentToolError(body, req)

	w.WriteHeader(resp.StatusCode)
	_, writeErr := w.Write(augmented)
	if writeErr == nil {
		writeErr = readErr
	}

	capture := augmented
	if len(capture) > maxCapturedErrorBodyBytes {
		capture = capture[:maxCapturedErrorBodyBytes]
	}
	return capture, writeErr
}

// augmentToolError enriches an Anthropic-style error JSON when the
// `error.message` references a specific tool by index. The wire
// shape is preserved (only the message text changes) so SDK error
// parsers keep working. If anything about the parse / lookup fails,
// the original body is returned untouched.
func augmentToolError(body []byte, req *ir.UnifiedRequest) []byte {
	if req == nil || len(req.Tools) == 0 || len(body) == 0 {
		return body
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(body, &raw); err != nil {
		return body
	}
	errRaw, ok := raw["error"]
	if !ok {
		return body
	}
	var errObj map[string]json.RawMessage
	if err := json.Unmarshal(errRaw, &errObj); err != nil {
		return body
	}
	msgRaw, ok := errObj["message"]
	if !ok {
		return body
	}
	var msg string
	if err := json.Unmarshal(msgRaw, &msg); err != nil {
		return body
	}
	idx, ok := parseToolIndex(msg)
	if !ok || idx < 0 || idx >= len(req.Tools) {
		return body
	}
	name := req.Tools[idx].Name
	if name == "" {
		return body
	}
	augmented, err := json.Marshal(fmt.Sprintf("[tool=%s] %s", name, msg))
	if err != nil {
		return body
	}
	errObj["message"] = augmented
	newErr, err := json.Marshal(errObj)
	if err != nil {
		return body
	}
	raw["error"] = newErr
	out, err := json.Marshal(raw)
	if err != nil {
		return body
	}
	return out
}

// parseToolIndex looks for "tools.N." in an error message and
// returns N. The pattern is what Anthropic uses for tool validation
// errors ("tools.0.custom.input_schema: ...").
func parseToolIndex(msg string) (int, bool) {
	const prefix = "tools."
	i := strings.Index(msg, prefix)
	if i < 0 {
		return 0, false
	}
	rest := msg[i+len(prefix):]
	end := 0
	for end < len(rest) && rest[end] >= '0' && rest[end] <= '9' {
		end++
	}
	if end == 0 || end >= len(rest) || rest[end] != '.' {
		return 0, false
	}
	n, err := strconv.Atoi(rest[:end])
	if err != nil {
		return 0, false
	}
	return n, true
}


// forwardBody reads the entire non-streaming response, extracts usage,
// and copies the body to the client. Reading the body fully (rather
// than io.Copy) lets us parse usage before writing — at the cost of
// holding the response in memory briefly, which is fine for chat
// responses (typically < 100 KB).
func forwardBody(w http.ResponseWriter, resp *http.Response) (usage.Usage, error) {
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return usage.Usage{}, fmt.Errorf("read upstream body: %w", err)
	}
	u := usage.FromAnthropicJSON(body)

	copySafeHeaders(w, resp)
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/json")
	}
	w.WriteHeader(resp.StatusCode)
	_, err = w.Write(body)
	return u, err
}

// forwardSSE pipes upstream SSE chunks to the client, flushing at
// every event boundary so tokens reach the client without buffering.
// A per-request SSESniffer reads the same lines on the side to
// extract usage. Returns (TTFB, usage, error).
func forwardSSE(
	w http.ResponseWriter,
	resp *http.Response,
	requestSentAt time.Time,
) (time.Duration, usage.Usage, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return 0, usage.Usage{}, errors.New("streaming requires http.Flusher")
	}

	copySafeHeaders(w, resp)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no") // disable nginx buffering
	w.WriteHeader(resp.StatusCode)

	reader := bufio.NewReaderSize(resp.Body, 64*1024)
	sniffer := usage.NewSSESniffer()
	var ttfb time.Duration

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			if ttfb == 0 {
				ttfb = time.Since(requestSentAt)
			}
			sniffer.Feed(line)

			if _, werr := w.Write(line); werr != nil {
				// Client disconnected. Keep reading from upstream
				// for a bounded window so the sniffer can capture
				// the final message_delta event — without this we
				// would record the message_start placeholder
				// output_tokens (~1–4) and the cost row would
				// under-bill an otherwise full generation.
				drainSSE(reader, sniffer, resp.Body)
				return ttfb, sniffer.Result(), fmt.Errorf("write client: %w", werr)
			}
			// Flush at SSE event boundary (blank line) to push tokens.
			if len(bytes.TrimRight(line, "\r\n")) == 0 {
				flusher.Flush()
			}
		}
		if errors.Is(err, io.EOF) {
			flusher.Flush()
			return ttfb, sniffer.Result(), nil
		}
		if err != nil {
			return ttfb, sniffer.Result(), fmt.Errorf("read upstream: %w", err)
		}
	}
}

// drainTimeout caps how long the gateway keeps reading from upstream
// after the client has disconnected. The drain exists purely to record
// the final usage event; anything past this is upstream taking
// abnormally long and we cut our losses.
const drainTimeout = 5 * time.Second

// drainMaxBytes belt-and-suspenders bounds the drain in case the
// timer fails to wake us (e.g. a transport that doesn't honour Close
// promptly). 256 KiB is enough for several closing SSE events.
const drainMaxBytes = 256 * 1024

// drainSSE continues reading SSE chunks past a client disconnect for
// up to drainTimeout / drainMaxBytes, feeding them into sniffer so
// the final message_delta event lands in usage.Result() before the
// cost row is persisted. Bytes are discarded — the client is gone.
//
// The timer-driven Close is what gives us a hard ceiling: ReadBytes
// blocks on the upstream socket, but once Close fires the next read
// returns immediately with an error. Without the timer a stalled
// upstream would pin one goroutine until the TCP keepalive expired.
func drainSSE(reader *bufio.Reader, sniffer *usage.SSESniffer, body io.Closer) {
	timer := time.AfterFunc(drainTimeout, func() { _ = body.Close() })
	defer timer.Stop()

	bytesRead := 0
	for bytesRead < drainMaxBytes {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			sniffer.Feed(line)
			bytesRead += len(line)
		}
		if err != nil {
			return
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

// IsRetriable reports whether an upstream HTTP status is worth
// retrying against a different account. 5xx (server errors) and 429
// (rate limit) qualify; 4xx other than 429 is a client problem and
// should be reflected back. Transport-level errors (returned from
// Dispatch with non-nil err) are always retriable.
func IsRetriable(status int) bool {
	return status >= 500 || status == http.StatusTooManyRequests
}
