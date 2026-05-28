package usage

// Parser extracts Usage from a provider's response wire format. The
// Forwarder selects a Parser based on the inbound protocol so token
// accounting works for both Anthropic and OpenAI shaped responses.
type Parser interface {
	// FromJSON extracts usage from a complete non-streaming body.
	FromJSON(body []byte) Usage
	// NewSniffer returns a fresh streaming usage extractor.
	NewSniffer() Sniffer
}

// Sniffer extracts Usage from streaming SSE lines fed one at a time.
type Sniffer interface {
	Feed(line []byte)
	Result() Usage
}

// Anthropic parses Anthropic Messages API responses. The Claude Platform
// driver shares this shape.
var Anthropic Parser = anthropicParser{}

// OpenAI parses OpenAI Chat Completions responses.
var OpenAI Parser = openaiParser{}

type anthropicParser struct{}

func (anthropicParser) FromJSON(body []byte) Usage { return FromAnthropicJSON(body) }
func (anthropicParser) NewSniffer() Sniffer        { return NewSSESniffer() }

type openaiParser struct{}

func (openaiParser) FromJSON(body []byte) Usage { return FromOpenAIJSON(body) }
func (openaiParser) NewSniffer() Sniffer        { return NewOpenAISniffer() }
