package tui

import (
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/bubbles/textinput"

	"github.com/lexcodex/relurpify/framework"
)

type fakeHITL struct {
	pending []*framework.PermissionRequest
	ch      chan framework.HITLEvent

	approved []string
	denied   []string
}

func newFakeHITL() *fakeHITL {
	return &fakeHITL{
		ch: make(chan framework.HITLEvent, 16),
	}
}

func (f *fakeHITL) PendingHITL() []*framework.PermissionRequest {
	out := make([]*framework.PermissionRequest, len(f.pending))
	copy(out, f.pending)
	return out
}

func (f *fakeHITL) ApproveHITL(requestID, _ string, _ framework.GrantScope, _ time.Duration) error {
	f.approved = append(f.approved, requestID)
	f.pending = removeRequest(f.pending, requestID)
	f.ch <- framework.HITLEvent{
		Type:    framework.HITLEventResolved,
		Request: &framework.PermissionRequest{ID: requestID},
		Decision: &framework.PermissionDecision{
			RequestID: requestID,
			Approved:  true,
		},
	}
	return nil
}

func (f *fakeHITL) DenyHITL(requestID, _ string) error {
	f.denied = append(f.denied, requestID)
	f.pending = removeRequest(f.pending, requestID)
	f.ch <- framework.HITLEvent{
		Type:    framework.HITLEventResolved,
		Request: &framework.PermissionRequest{ID: requestID},
		Decision: &framework.PermissionDecision{
			RequestID: requestID,
			Approved:  false,
			Reason:    "denied",
		},
	}
	return nil
}

func (f *fakeHITL) SubscribeHITL() (<-chan framework.HITLEvent, func()) {
	return f.ch, func() {}
}

func removeRequest(reqs []*framework.PermissionRequest, id string) []*framework.PermissionRequest {
	for i, r := range reqs {
		if r != nil && r.ID == id {
			return append(reqs[:i], reqs[i+1:]...)
		}
	}
	return reqs
}

func TestHITLPromptApproveFlow(t *testing.T) {
	hitl := newFakeHITL()
	req := &framework.PermissionRequest{
		ID:            "hitl-1",
		Permission:    framework.PermissionDescriptor{Action: "file_matrix:write", Resource: "src/main.rs"},
		Justification: "file permission matrix",
	}
	hitl.pending = []*framework.PermissionRequest{req}

	input := textinput.New()
	input.Focus()

	m := Model{
		hitl:    hitl,
		hitlCh:  hitl.ch,
		input:   input,
		mode:    ModeNormal,
		messages: []Message{},
	}

	updatedAny, _ := m.Update(hitlEventMsg{event: framework.HITLEvent{Type: framework.HITLEventRequested, Request: req}})
	updated := updatedAny.(Model)
	if updated.mode != ModeHITL {
		t.Fatalf("expected ModeHITL, got %v", updated.mode)
	}
	if updated.hitlRequest == nil || updated.hitlRequest.ID != "hitl-1" {
		t.Fatalf("expected hitlRequest hitl-1, got %+v", updated.hitlRequest)
	}

	// Press "y" and execute returned command.
	modelAny, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	model := modelAny.(Model)
	if cmd == nil {
		t.Fatalf("expected approve cmd")
	}
	msg := cmd()

	modelAny2, _ := model.Update(msg)
	model2 := modelAny2.(Model)
	if model2.mode == ModeHITL {
		t.Fatalf("expected HITL mode exited")
	}
	if len(hitl.approved) != 1 || hitl.approved[0] != "hitl-1" {
		t.Fatalf("expected approved hitl-1, got %v", hitl.approved)
	}
}

func TestHITLPromptDenyFlow(t *testing.T) {
	hitl := newFakeHITL()
	req := &framework.PermissionRequest{
		ID:            "hitl-2",
		Permission:    framework.PermissionDescriptor{Action: "bash:exec", Resource: "cargo build"},
		Justification: "bash permission policy",
	}
	hitl.pending = []*framework.PermissionRequest{req}

	input := textinput.New()
	input.Focus()

	m := Model{
		hitl:    hitl,
		hitlCh:  hitl.ch,
		input:   input,
		mode:    ModeNormal,
		messages: []Message{},
	}

	updatedAny, _ := m.Update(hitlEventMsg{event: framework.HITLEvent{Type: framework.HITLEventRequested, Request: req}})
	updated := updatedAny.(Model)
	if updated.mode != ModeHITL {
		t.Fatalf("expected ModeHITL, got %v", updated.mode)
	}

	modelAny, cmd := updated.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	model := modelAny.(Model)
	if cmd == nil {
		t.Fatalf("expected deny cmd")
	}
	msg := cmd()

	modelAny2, _ := model.Update(msg)
	model2 := modelAny2.(Model)
	if model2.mode == ModeHITL {
		t.Fatalf("expected HITL mode exited")
	}
	if len(hitl.denied) != 1 || hitl.denied[0] != "hitl-2" {
		t.Fatalf("expected denied hitl-2, got %v", hitl.denied)
	}
}

