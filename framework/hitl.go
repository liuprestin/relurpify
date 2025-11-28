package framework

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// RiskLevel models the qualitative assessment required by the HITL flow.
type RiskLevel string

const (
	RiskLevelLow    RiskLevel = "low"
	RiskLevelMedium RiskLevel = "medium"
	RiskLevelHigh   RiskLevel = "high"
)

// GrantScope defines the lifecycle of an approval.
type GrantScope string

const (
	GrantScopeOneTime     GrantScope = "one_time"
	GrantScopeSession     GrantScope = "session"
	GrantScopePersistent  GrantScope = "persistent"
	GrantScopeConditional GrantScope = "conditional"
)

// PermissionRequest captures a pending permission escalation.
type PermissionRequest struct {
	ID            string               `json:"id"`
	Permission    PermissionDescriptor `json:"permission"`
	Justification string               `json:"justification"`
	Scope         GrantScope           `json:"scope"`
	Duration      time.Duration        `json:"duration"`
	Risk          RiskLevel            `json:"risk"`
	RequestedAt   time.Time            `json:"requested_at"`
	State         string               `json:"state"`
}

// PermissionDecision encapsulates an approval or rejection.
type PermissionDecision struct {
	RequestID  string            `json:"request_id"`
	Approved   bool              `json:"approved"`
	ApprovedBy string            `json:"approved_by"`
	Scope      GrantScope        `json:"scope"`
	ExpiresAt  time.Time         `json:"expires_at"`
	Reason     string            `json:"reason,omitempty"`
	Conditions map[string]string `json:"conditions,omitempty"`
}

// HITLBroker coordinates blocking and async approvals.
type HITLBroker struct {
	timeout  time.Duration
	mu       sync.Mutex
	requests map[string]*PermissionRequest
	waiters  map[string]chan PermissionDecision
	clock    func() time.Time
}

// NewHITLBroker builds a broker with the supplied timeout.
func NewHITLBroker(timeout time.Duration) *HITLBroker {
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	return &HITLBroker{
		timeout:  timeout,
		requests: make(map[string]*PermissionRequest),
		waiters:  make(map[string]chan PermissionDecision),
		clock:    time.Now,
	}
}

// RequestPermission registers a request and waits for approval when possible.
func (h *HITLBroker) RequestPermission(ctx context.Context, req PermissionRequest) (*PermissionGrant, error) {
	if req.Permission.Action == "" {
		return nil, errors.New("permission request missing action")
	}
	req.ID = fmt.Sprintf("hitl-%d", h.clock().UnixNano())
	req.RequestedAt = h.clock()
	req.State = "pending"

	waitCh := make(chan PermissionDecision, 1)

	h.mu.Lock()
	h.requests[req.ID] = &req
	h.waiters[req.ID] = waitCh
	h.mu.Unlock()

	select {
	case decision := <-waitCh:
		deleteFn := func() {
			h.mu.Lock()
			delete(h.requests, req.ID)
			delete(h.waiters, req.ID)
			h.mu.Unlock()
		}
		defer deleteFn()
		if !decision.Approved {
			return nil, fmt.Errorf("permission denied: %s", decision.Reason)
		}
		return &PermissionGrant{
			ID:          decision.RequestID,
			Permission:  req.Permission,
			Scope:       decision.Scope,
			ApprovedBy:  decision.ApprovedBy,
			Conditions:  decision.Conditions,
			GrantedAt:   h.clock(),
			ExpiresAt:   decision.ExpiresAt,
			Description: req.Justification,
		}, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-time.After(h.timeout):
		return nil, fmt.Errorf("permission request %s timed out", req.Permission.Action)
	}
}

// SubmitAsync registers a request without blocking.
func (h *HITLBroker) SubmitAsync(req PermissionRequest) (string, error) {
	req.ID = fmt.Sprintf("hitl-%d", h.clock().UnixNano())
	req.RequestedAt = h.clock()
	req.State = "pending"
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, exists := h.requests[req.ID]; exists {
		return "", fmt.Errorf("request %s already registered", req.ID)
	}
	h.requests[req.ID] = &req
	h.waiters[req.ID] = make(chan PermissionDecision, 1)
	return req.ID, nil
}

// Approve asynchronously approves a request.
func (h *HITLBroker) Approve(decision PermissionDecision) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	req, ok := h.requests[decision.RequestID]
	if !ok {
		return fmt.Errorf("request %s not found", decision.RequestID)
	}
	req.State = "approved"
	if decision.Scope == "" {
		decision.Scope = req.Scope
	}
	if decision.ExpiresAt.IsZero() && decision.Scope == GrantScopeOneTime {
		decision.ExpiresAt = h.clock().Add(time.Minute)
	}
	if waiter, ok := h.waiters[decision.RequestID]; ok {
		waiter <- decision
		close(waiter)
	}
	return nil
}

// Deny rejects a request.
func (h *HITLBroker) Deny(requestID, reason string) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	req, ok := h.requests[requestID]
	if !ok {
		return fmt.Errorf("request %s not found", requestID)
	}
	req.State = "denied"
	if waiter, ok := h.waiters[requestID]; ok {
		waiter <- PermissionDecision{
			RequestID: requestID,
			Approved:  false,
			Reason:    reason,
		}
		close(waiter)
	}
	return nil
}

// PendingRequests returns the outstanding approvals.
func (h *HITLBroker) PendingRequests() []*PermissionRequest {
	h.mu.Lock()
	defer h.mu.Unlock()
	var pending []*PermissionRequest
	for _, req := range h.requests {
		if req.State == "pending" {
			pending = append(pending, req)
		}
	}
	return pending
}

// GrantManual creates a permission grant without the async flow.
func GrantManual(permission PermissionDescriptor, approvedBy string, scope GrantScope, duration time.Duration) *PermissionGrant {
	return &PermissionGrant{
		ID:         fmt.Sprintf("manual-%d", time.Now().UnixNano()),
		Permission: permission,
		Scope:      scope,
		ApprovedBy: approvedBy,
		GrantedAt:  time.Now().UTC(),
		ExpiresAt:  time.Now().Add(duration),
	}
}
