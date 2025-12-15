package tui

import (
	"fmt"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	runtimesvc "github.com/lexcodex/relurpify/app/relurpish/runtime"
	"github.com/lexcodex/relurpify/framework"
)

type hitlService interface {
	PendingHITL() []*framework.PermissionRequest
	ApproveHITL(requestID, approver string, scope framework.GrantScope, duration time.Duration) error
	DenyHITL(requestID, reason string) error
	SubscribeHITL() (<-chan framework.HITLEvent, func())
}

func hitlServiceFromRuntime(rt *runtimesvc.Runtime) hitlService {
	if rt == nil {
		return nil
	}
	return rt
}

type hitlEventMsg struct{ event framework.HITLEvent }

func listenHITLEvents(ch <-chan framework.HITLEvent) tea.Cmd {
	if ch == nil {
		return nil
	}
	return func() tea.Msg {
		ev, ok := <-ch
		if !ok {
			return nil
		}
		return hitlEventMsg{event: ev}
	}
}

type hitlResolvedMsg struct {
	requestID string
	approved  bool
	err       error
}

func approveHITLCmd(svc hitlService, requestID string) tea.Cmd {
	return func() tea.Msg {
		if svc == nil {
			return hitlResolvedMsg{requestID: requestID, approved: true, err: fmt.Errorf("hitl service unavailable")}
		}
		err := svc.ApproveHITL(requestID, "tui", framework.GrantScopeOneTime, 5*time.Minute)
		return hitlResolvedMsg{requestID: requestID, approved: true, err: err}
	}
}

func denyHITLCmd(svc hitlService, requestID string) tea.Cmd {
	return func() tea.Msg {
		if svc == nil {
			return hitlResolvedMsg{requestID: requestID, approved: false, err: fmt.Errorf("hitl service unavailable")}
		}
		err := svc.DenyHITL(requestID, "denied in TUI")
		return hitlResolvedMsg{requestID: requestID, approved: false, err: err}
	}
}

