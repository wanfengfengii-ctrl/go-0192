package service

import (
	"context"
	"fmt"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/policy"
	"quorumforge/internal/store"
)

// TerminalResult is returned by seal, quarantine and cancel.
type TerminalResult struct {
	State    ceremony.State    `json:"state"`
	Revision ceremony.Revision `json:"revision"`
	Reason   string            `json:"reason,omitempty"`
}

// SealRequest seals the reviewed and approved signature product.
type SealRequest struct {
	CeremonyID ceremony.ID       `json:"ceremony_id"`
	Operation  string            `json:"operation"`
	Revision   ceremony.Revision `json:"revision"`
	Reason     string            `json:"reason"`
}

// Seal requires quorum, an approved review conclusion and the unique signature
// artifact all to hold, then transitions the ceremony to the sealed terminal.
func (s *Service) Seal(ctx context.Context, req SealRequest) (TerminalResult, error) {
	if req.CeremonyID == "" || req.Operation == "" {
		return TerminalResult{}, fmt.Errorf("%w: missing required fields", ErrInvalidArgument)
	}

	threshold, err := s.thresholdFor(ctx, req.CeremonyID)
	if err != nil {
		return TerminalResult{}, err
	}

	return runCommand(s, ctx, commandSpec{
		id:        req.CeremonyID,
		operation: req.Operation,
		kind:      "seal",
		content:   contentOf(req.Reason),
	}, func(tx store.Tx) (TerminalResult, error) {
		c, err := tx.LoadCeremony(ctx, req.CeremonyID)
		if err != nil {
			return TerminalResult{}, err
		}
		if c.State != ceremony.StatePendingReview {
			return TerminalResult{}, fmt.Errorf("seal not allowed in state %s", c.State)
		}
		if err := c.GuardRevision(req.Revision); err != nil {
			return TerminalResult{}, err
		}

		witnesses, err := tx.LoadWitnesses(ctx, req.CeremonyID)
		if err != nil {
			return TerminalResult{}, err
		}
		if len(witnesses) < threshold {
			return TerminalResult{}, fmt.Errorf("%w: %d/%d", ErrQuorumNotReached, len(witnesses), threshold)
		}

		if !c.ReviewConclusion.Approved() {
			return TerminalResult{}, fmt.Errorf("seal requires an approved review, got %q", c.ReviewConclusion)
		}

		sess, err := tx.LoadSession(ctx, req.CeremonyID)
		if err != nil || sess == nil {
			return TerminalResult{}, fmt.Errorf("%w", ErrNoSession)
		}
		if sess.Receipt == "" {
			return TerminalResult{}, fmt.Errorf("seal requires an HSM receipt")
		}

		if _, err := tx.LoadArtifact(ctx, req.CeremonyID); err != nil {
			return TerminalResult{}, fmt.Errorf("seal requires a signature artifact")
		}

		reason := req.Reason
		if reason == "" {
			reason = "sealed"
		}
		if err := c.Transition(ceremony.StateSealed); err != nil {
			return TerminalResult{}, err
		}
		c.TerminalReason = reason
		c.BumpRevision()
		if err := tx.SaveCeremony(c); err != nil {
			return TerminalResult{}, err
		}

		return TerminalResult{State: c.State, Revision: c.Revision, Reason: reason}, nil
	})
}

// QuarantineRequest isolates a ceremony.
type QuarantineRequest struct {
	CeremonyID ceremony.ID       `json:"ceremony_id"`
	Operation  string            `json:"operation"`
	Revision   ceremony.Revision `json:"revision"`
	Reason     string            `json:"reason"`
}

// Quarantine transitions a non-terminal ceremony into the isolated terminal.
func (s *Service) Quarantine(ctx context.Context, req QuarantineRequest) (TerminalResult, error) {
	if req.CeremonyID == "" || req.Operation == "" {
		return TerminalResult{}, fmt.Errorf("%w: missing required fields", ErrInvalidArgument)
	}

	return runCommand(s, ctx, commandSpec{
		id:        req.CeremonyID,
		operation: req.Operation,
		kind:      "quarantine",
		content:   contentOf(req.Reason),
	}, func(tx store.Tx) (TerminalResult, error) {
		c, err := tx.LoadCeremony(ctx, req.CeremonyID)
		if err != nil {
			return TerminalResult{}, err
		}
		if err := c.GuardRevision(req.Revision); err != nil {
			return TerminalResult{}, err
		}
		reason := req.Reason
		if reason == "" {
			reason = "quarantined"
		}
		if err := c.Transition(ceremony.StateQuarantined); err != nil {
			return TerminalResult{}, err
		}
		c.TerminalReason = reason
		c.BumpRevision()
		if err := tx.SaveCeremony(c); err != nil {
			return TerminalResult{}, err
		}
		return TerminalResult{State: c.State, Revision: c.Revision, Reason: reason}, nil
	})
}

// CancelRequest cancels a ceremony.
type CancelRequest struct {
	CeremonyID ceremony.ID       `json:"ceremony_id"`
	Operation  string            `json:"operation"`
	Revision   ceremony.Revision `json:"revision"`
	Reason     string            `json:"reason"`
}

// Cancel transitions a non-terminal ceremony into the cancelled terminal.
func (s *Service) Cancel(ctx context.Context, req CancelRequest) (TerminalResult, error) {
	if req.CeremonyID == "" || req.Operation == "" {
		return TerminalResult{}, fmt.Errorf("%w: missing required fields", ErrInvalidArgument)
	}

	return runCommand(s, ctx, commandSpec{
		id:        req.CeremonyID,
		operation: req.Operation,
		kind:      "cancel",
		content:   contentOf(req.Reason),
	}, func(tx store.Tx) (TerminalResult, error) {
		c, err := tx.LoadCeremony(ctx, req.CeremonyID)
		if err != nil {
			return TerminalResult{}, err
		}
		if err := c.GuardRevision(req.Revision); err != nil {
			return TerminalResult{}, err
		}
		reason := req.Reason
		if reason == "" {
			reason = "cancelled"
		}
		if err := c.Transition(ceremony.StateCancelled); err != nil {
			return TerminalResult{}, err
		}
		c.TerminalReason = reason
		c.BumpRevision()
		if err := tx.SaveCeremony(c); err != nil {
			return TerminalResult{}, err
		}
		return TerminalResult{State: c.State, Revision: c.Revision, Reason: reason}, nil
	})
}

// thresholdFor loads the quorum threshold for a ceremony's policy version.
func (s *Service) thresholdFor(ctx context.Context, id ceremony.ID) (int, error) {
	c, err := s.store.LoadCeremony(ctx, id)
	if err != nil {
		return 0, err
	}
	p, err := s.store.GetPolicy(ctx, policy.Version(c.PolicyVersion))
	if err != nil {
		return 0, err
	}
	return p.Threshold, nil
}
