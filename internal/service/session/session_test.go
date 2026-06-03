package session_test

import (
	"testing"
	"time"

	"github.com/jami1024/omnihub/internal/ir"
	"github.com/jami1024/omnihub/internal/service/session"
)

func TestKeyStableAcrossTurns(t *testing.T) {
	turn1 := &ir.UnifiedRequest{
		Model: "claude-haiku-4-5",
		System: []ir.ContentBlock{
			{Type: ir.BlockText, Text: "You are a helpful assistant."},
		},
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{
				{Type: ir.BlockText, Text: "Hello, who are you?"},
			}},
		},
	}
	turn2 := &ir.UnifiedRequest{
		Model:  "claude-haiku-4-5",
		System: turn1.System,
		Messages: []ir.Message{
			turn1.Messages[0],
			{Role: ir.RoleAssistant, Content: []ir.ContentBlock{{Type: ir.BlockText, Text: "I'm Claude."}}},
			{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.BlockText, Text: "Continue."}}},
		},
	}

	k1 := session.KeyFor("alice", turn1)
	k2 := session.KeyFor("alice", turn2)
	if k1 == "" || k2 == "" {
		t.Fatalf("KeyFor returned empty key: k1=%q k2=%q", k1, k2)
	}
	if k1 != k2 {
		t.Errorf("session key should be stable across turns: %q vs %q", k1, k2)
	}
}

func TestKeyDifferentiatesVirtualKeys(t *testing.T) {
	req := &ir.UnifiedRequest{
		Model: "claude-haiku-4-5",
		Messages: []ir.Message{
			{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.BlockText, Text: "hi"}}},
		},
	}
	if session.KeyFor("alice", req) == session.KeyFor("bob", req) {
		t.Error("alice and bob with the same prompt should still get distinct session keys")
	}
}

func TestKeyDifferentiatesSystemPrompts(t *testing.T) {
	make := func(system string) *ir.UnifiedRequest {
		return &ir.UnifiedRequest{
			Model:  "claude-haiku-4-5",
			System: []ir.ContentBlock{{Type: ir.BlockText, Text: system}},
			Messages: []ir.Message{
				{Role: ir.RoleUser, Content: []ir.ContentBlock{{Type: ir.BlockText, Text: "hi"}}},
			},
		}
	}
	a := session.KeyFor("alice", make("act as a doctor"))
	b := session.KeyFor("alice", make("act as a lawyer"))
	if a == b {
		t.Error("different system prompts should produce different session keys")
	}
}

func TestKeyEmptyWhenNothingToHash(t *testing.T) {
	req := &ir.UnifiedRequest{Model: "claude-haiku-4-5"} // no system, no messages
	if got := session.KeyFor("alice", req); got != "" {
		t.Errorf("empty request should yield empty session key, got %q", got)
	}
}

func TestStoreBindAndGet(t *testing.T) {
	s := session.New(50 * time.Millisecond)
	s.Bind("k", 42)

	if got, ok := s.Get("k"); !ok || got != 42 {
		t.Errorf("Get: want (42, true), got (%d, %v)", got, ok)
	}
}

func TestStoreGetUnknownReturnsFalse(t *testing.T) {
	s := session.New(time.Minute)
	if _, ok := s.Get("missing"); ok {
		t.Error("Get on unknown key should return false")
	}
	if _, ok := s.Get(""); ok {
		t.Error("Get on empty key should return false")
	}
}

func TestStoreExpiresAfterTTL(t *testing.T) {
	s := session.New(20 * time.Millisecond)
	s.Bind("k", 1)

	time.Sleep(40 * time.Millisecond)
	if _, ok := s.Get("k"); ok {
		t.Error("entry should have expired after TTL")
	}
	if s.Size() != 0 {
		t.Errorf("expired entry should have been dropped on Get, size=%d", s.Size())
	}
}

func TestStoreBindRefreshesTTL(t *testing.T) {
	s := session.New(50 * time.Millisecond)
	s.Bind("k", 1)
	time.Sleep(30 * time.Millisecond)
	s.Bind("k", 1) // refresh
	time.Sleep(30 * time.Millisecond)

	if _, ok := s.Get("k"); !ok {
		t.Error("Bind should refresh the TTL")
	}
}

func TestStoreDropRemoves(t *testing.T) {
	s := session.New(time.Minute)
	s.Bind("k", 1)
	s.Drop("k")
	if _, ok := s.Get("k"); ok {
		t.Error("Drop should remove the binding")
	}
}

func TestStoreBindIgnoresInvalidInput(t *testing.T) {
	s := session.New(time.Minute)
	s.Bind("", 1)   // empty key
	s.Bind("k", 0)  // bad id
	s.Bind("k", -1) // negative id
	if s.Size() != 0 {
		t.Errorf("invalid bindings should be ignored, size=%d", s.Size())
	}
}
