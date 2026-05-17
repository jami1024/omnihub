package anthropic

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// DecodeStream returns an iterator over IR chunks decoded from
// Anthropic Server-Sent Events.
func (d *Driver) DecodeStream(body io.ReadCloser) provider.StreamIter {
	return &streamIter{
		body:   body,
		reader: bufio.NewReaderSize(body, 64*1024),
	}
}

// streamIter implements provider.StreamIter for Anthropic SSE.
//
// Wire format (Anthropic):
//
//	event: message_start
//	data: {"type":"message_start","message":{...}}
//
//	event: content_block_delta
//	data: {"type":"content_block_delta","index":0,"delta":{...}}
//
//	...
//
// Each "data:" line carries a complete JSON object that round-trips
// directly into ir.UnifiedChunk because the IR was modelled after this
// shape. The "event:" line is informational and can be skipped — the
// JSON payload contains the same type.
type streamIter struct {
	body   io.ReadCloser
	reader *bufio.Reader
	closed bool
	err    error
}

// Next returns the next chunk, or io.EOF when the stream is exhausted.
func (s *streamIter) Next() (*ir.UnifiedChunk, error) {
	if s.closed {
		return nil, io.EOF
	}
	for {
		line, err := s.reader.ReadBytes('\n')
		if len(line) == 0 && err != nil {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			return nil, fmt.Errorf("anthropic stream: read: %w", err)
		}
		line = bytes.TrimRight(line, "\r\n")

		// Blank line, comment (":..."), or event-type line: skip.
		if len(line) == 0 || line[0] == ':' || bytes.HasPrefix(line, []byte("event:")) || bytes.HasPrefix(line, []byte("id:")) || bytes.HasPrefix(line, []byte("retry:")) {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			continue
		}

		if !bytes.HasPrefix(line, []byte("data:")) {
			// Unknown SSE field; skip rather than fail.
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			continue
		}

		payload := bytes.TrimSpace(line[len("data:"):])
		if len(payload) == 0 {
			if errors.Is(err, io.EOF) {
				return nil, io.EOF
			}
			continue
		}

		var chunk ir.UnifiedChunk
		if jerr := json.Unmarshal(payload, &chunk); jerr != nil {
			return nil, fmt.Errorf("anthropic stream: decode data: %w (payload=%s)", jerr, payload)
		}
		return &chunk, nil
	}
}

// Close releases the underlying body. Safe to call more than once.
func (s *streamIter) Close() error {
	if s.closed {
		return s.err
	}
	s.closed = true
	if s.body != nil {
		s.err = s.body.Close()
	}
	return s.err
}
