package guard

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestClientGate(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name       string
		spec       string
		ua         string
		headers    map[string]string
		wantStatus int
		wantReason string // substring expected in body when rejected
	}{
		{
			name:       "real claude cli with all signals passes",
			ua:         "claude-cli/2.1.92 (external, cli)",
			headers:    map[string]string{"X-App": "cli", "Anthropic-Beta": "max-tokens-3-5-sonnet-2024-07-15"},
			wantStatus: 200,
		},
		{
			name:       "curl rejected on UA prefix",
			ua:         "curl/8.4.0",
			wantStatus: 403,
			wantReason: "does not accept this client",
		},
		{
			name:       "spoofed UA without X-App rejected",
			ua:         "claude-cli/2.1.92",
			headers:    map[string]string{"Anthropic-Beta": "x"},
			wantStatus: 403,
			wantReason: `\"X-App\"`,
		},
		{
			name:       "spoofed UA without Anthropic-Beta rejected",
			ua:         "claude-cli/2.1.92",
			headers:    map[string]string{"X-App": "cli"},
			wantStatus: 403,
			wantReason: `\"Anthropic-Beta\"`,
		},
		{
			name:       "wrong X-App value rejected",
			ua:         "claude-cli/2.1.92",
			headers:    map[string]string{"X-App": "vscode", "Anthropic-Beta": "x"},
			wantStatus: 403,
			wantReason: `\"vscode\"`,
		},
		{
			name:       "star opens the gate even for curl",
			spec:       "*",
			ua:         "curl/8.4.0",
			wantStatus: 200,
		},
		{
			name:       "custom prefix bypasses signal check",
			spec:       "codex-cli/",
			ua:         "codex-cli/0.9",
			wantStatus: 200,
		},
		{
			name:       "non-matching UA still rejected with custom prefix",
			spec:       "codex-cli/",
			ua:         "claude-cli/2.1.92",
			wantStatus: 403,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gate := NewClientGate(tt.spec)
			r := gin.New()
			r.Use(gate.Middleware())
			r.POST("/x", func(c *gin.Context) { c.Status(200) })

			req := httptest.NewRequest("POST", "/x", nil)
			req.Header.Set("User-Agent", tt.ua)
			for k, v := range tt.headers {
				req.Header.Set(k, v)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d. body=%s", w.Code, tt.wantStatus, w.Body.String())
			}
			if tt.wantReason != "" && !strings.Contains(w.Body.String(), tt.wantReason) {
				t.Fatalf("body missing %q. body=%s", tt.wantReason, w.Body.String())
			}
		})
	}
}

func TestClientGateIsOpen(t *testing.T) {
	if NewClientGate("*").IsOpen() != true {
		t.Fatal(`"*" spec should produce an open gate`)
	}
	if NewClientGate("").IsOpen() != false {
		t.Fatal("empty spec should produce default (closed) gate")
	}
	if NewClientGate("claude-cli/,codex-cli/").IsOpen() != false {
		t.Fatal("explicit prefix list should produce a closed gate")
	}
}
