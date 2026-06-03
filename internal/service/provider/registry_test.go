package provider_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/provider"
)

// stubDriver is a minimal Driver implementation used only to exercise
// the Registry.
type stubDriver struct{ name string }

func (s *stubDriver) Name() string                        { return s.name }
func (s *stubDriver) Capabilities() provider.Capabilities { return provider.Capabilities{Chat: true} }
func (s *stubDriver) BuildRequest(context.Context, *ir.UnifiedRequest, *provider.Account) (*http.Request, error) {
	return nil, nil
}
func (s *stubDriver) ParseResponse(*http.Response) (*ir.UnifiedResponse, error) { return nil, nil }
func (s *stubDriver) DecodeStream(io.ReadCloser) provider.StreamIter            { return nil }

func TestRegistryRegisterAndGet(t *testing.T) {
	r := provider.NewRegistry()
	d := &stubDriver{name: "anthropic"}

	if err := r.Register(d); err != nil {
		t.Fatalf("register: %v", err)
	}

	got, ok := r.Get("anthropic")
	if !ok {
		t.Fatalf("Get: driver not found")
	}
	if got != d {
		t.Fatalf("Get: returned wrong instance")
	}

	if _, ok := r.Get("unknown"); ok {
		t.Fatalf("Get: unexpected hit on unknown name")
	}
}

func TestRegistryRejectsDuplicate(t *testing.T) {
	r := provider.NewRegistry()
	_ = r.Register(&stubDriver{name: "anthropic"})

	err := r.Register(&stubDriver{name: "anthropic"})
	if err == nil {
		t.Fatalf("expected duplicate registration to fail")
	}
}

func TestRegistryRejectsNilOrEmpty(t *testing.T) {
	r := provider.NewRegistry()

	if err := r.Register(nil); err == nil {
		t.Errorf("expected nil driver to be rejected")
	}
	if err := r.Register(&stubDriver{name: ""}); err == nil {
		t.Errorf("expected empty-name driver to be rejected")
	}
}

func TestRegistryNamesSorted(t *testing.T) {
	r := provider.NewRegistry()
	_ = r.Register(&stubDriver{name: "openai"})
	_ = r.Register(&stubDriver{name: "anthropic"})
	_ = r.Register(&stubDriver{name: "bedrock"})

	names := r.Names()
	want := []string{"anthropic", "bedrock", "openai"}
	if len(names) != len(want) {
		t.Fatalf("Names len: want %d, got %d (%v)", len(want), len(names), names)
	}
	for i, n := range want {
		if names[i] != n {
			t.Errorf("Names[%d]: want %q, got %q", i, n, names[i])
		}
	}
}

func TestMustRegisterPanicsOnDuplicate(t *testing.T) {
	r := provider.NewRegistry()
	r.MustRegister(&stubDriver{name: "anthropic"})

	defer func() {
		if recover() == nil {
			t.Errorf("expected panic on duplicate MustRegister")
		}
	}()
	r.MustRegister(&stubDriver{name: "anthropic"})
}

func TestAccountCredentialAccessor(t *testing.T) {
	a := &provider.Account{Credentials: map[string]string{"api_key": "sk-test"}}

	if got := a.Credential("api_key"); got != "sk-test" {
		t.Errorf("Credential(api_key): want sk-test, got %q", got)
	}
	if got := a.Credential("missing"); got != "" {
		t.Errorf("Credential(missing): want empty, got %q", got)
	}

	var nilAccount *provider.Account
	if got := nilAccount.Credential("api_key"); got != "" {
		t.Errorf("nil receiver: want empty, got %q", got)
	}
}

// Ensure unused-import safety: errors and stubDriver pkgs reachable.
var _ = errors.New
