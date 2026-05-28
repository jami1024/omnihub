package openai

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"

	"github.com/jami1024/omnihub/internal/ir"
	protoopenai "github.com/jami1024/omnihub/internal/protocol/openai"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// DecodeStream returns an iterator over IR chunks decoded from OpenAI
// Server-Sent Events.
func (d *Driver) DecodeStream(body io.ReadCloser) provider.StreamIter {
	return &streamIter{
		body:   body,
		reader: bufio.NewReaderSize(body, 64*1024),
	}
}

// streamIter implements provider.StreamIter for OpenAI SSE.
//
// Wire format (OpenAI): each event is a single "data:" line carrying a
// complete chat.completion.chunk JSON object, terminated by a sentinel
// "data: [DONE]" line. There are no "event:" lines.
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
			return nil, fmt.Errorf("openai stream: read: %w", err)
		}
		line = bytes.TrimRight(line, "\r\n")

		if len(line) == 0 || line[0] == ':' || !bytes.HasPrefix(line, []byte("data:")) {
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
		if bytes.Equal(payload, []byte("[DONE]")) {
			return nil, io.EOF
		}

		chunk, jerr := protoopenai.ChunkToIR(payload)
		if jerr != nil {
			return nil, fmt.Errorf("openai stream: %w", jerr)
		}
		return chunk, nil
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
