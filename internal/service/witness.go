package service

import (
	"context"
	"fmt"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/policy"
	"quorumforge/internal/store"
)

// ConfirmRequest records a witness confirmation toward quorum.
type ConfirmRequest struct {
	CeremonyID       ceremony.ID       `json:"ceremony_id"`
	Operation        string            `json:"operation"`
	Revision         ceremony.Revision `json:"revision"`
	PersonID         string            `json:"person_id"`
	Credential       string            `json:"credential"`
	IdentityRevision uint64            `json:"identity_revision"`
}

// ConfirmResult is returned after a witness confirmation.
type ConfirmResult struct {
	PersonID      string            `json:"person_id"`
	WitnessCount  int               `json:"witness_count"`
	Threshold     int               `json:"threshold"`
	QuorumReached bool              `json:"quorum_reached"`
	State         ceremony.State    `json:"state"`
	Revision      ceremony.Revision `json:"revision"`
}

// ConfirmWitness validates the identity and credential, then records a
// de-duplicated witness confirmation and advances quorum when reached.
func (s *Service) ConfirmWitness(ctx context.Context, req ConfirmRequest) (ConfirmResult, error) {
	if req.CeremonyID == "" || req.Operation == "" || req.PersonID == "" {
		return ConfirmResult{}, fmt.Errorf("%w: missing required fields", ErrInvalidArgument)
	}
	if req.Credential == "" {
		return ConfirmResult{}, fmt.Errorf("%w: missing credential", ErrInvalidArgument)
	}

	// Resolve the current identity up front (read-only directory lookup).
	part, err := s.store.GetParticipant(ctx, policy.PersonID(req.PersonID))
	if err != nil {
		if err == store.ErrNotFound {
			return ConfirmResult{}, fmt.Errorf("%w: %s", ErrIdentityMismatch, req.PersonID)
		}
		return ConfirmResult{}, err
	}
	if !part.Active() {
		return ConfirmResult{}, fmt.Errorf("%w: %s", ErrRevoked, req.PersonID)
	}
	if !part.HasCredential(req.Credential) {
		return ConfirmResult{}, fmt.Errorf("%w: credential not registered for %s", ErrInvalidArgument, req.PersonID)
	}

	content := contentOf(req.PersonID, req.Credential, fmt.Sprintf("%d", req.IdentityRevision))

	// Resolve the quorum threshold up front (read-only directory lookup).
	threshold := 0
	if c, err := s.store.LoadCeremony(ctx, req.CeremonyID); err == nil && c.PolicyVersion != "" {
		if p, err := s.store.GetPolicy(ctx, policy.Version(c.PolicyVersion)); err == nil {
			threshold = p.Threshold
		}
	}

	return runCommand(s, ctx, commandSpec{
		id:        req.CeremonyID,
		operation: req.Operation,
		kind:      "confirm-witness",
		content:   content,
	}, func(tx store.Tx) (ConfirmResult, error) {
		c, err := tx.LoadCeremony(ctx, req.CeremonyID)
		if err != nil {
			return ConfirmResult{}, err
		}
		if c.State != ceremony.StateGatheringWitnesses {
			return ConfirmResult{}, fmt.Errorf("witness confirmation not allowed in state %s", c.State)
		}
		if err := c.GuardRevision(req.Revision); err != nil {
			return ConfirmResult{}, err
		}

		scope, err := tx.LoadScope(ctx, req.CeremonyID)
		if err != nil {
			return ConfirmResult{}, err
		}
		var frozenRevision uint64
		found := false
		for _, e := range scope {
			if e.PersonID == req.PersonID {
				frozenRevision = e.Revision
				found = true
				break
			}
		}
		if !found {
			return ConfirmResult{}, fmt.Errorf("%w: %s not in frozen scope", ErrIdentityMismatch, req.PersonID)
		}
		if req.IdentityRevision != frozenRevision {
			return ConfirmResult{}, fmt.Errorf("%w: %s referenced revision %d want %d", ErrIdentityMismatch, req.PersonID, req.IdentityRevision, frozenRevision)
		}
		if part.Revision != frozenRevision {
			return ConfirmResult{}, fmt.Errorf("%w: %s identity changed since lock", ErrIdentityMismatch, req.PersonID)
		}

		existing, err := tx.LoadWitnesses(ctx, req.CeremonyID)
		if err != nil {
			return ConfirmResult{}, err
		}
		for _, w := range existing {
			if w.PersonID == req.PersonID {
				return ConfirmResult{}, fmt.Errorf("%w: %s already confirmed", ErrDuplicateWitness, req.PersonID)
			}
		}

		if err := tx.SaveWitness(req.CeremonyID, store.WitnessRecord{
			PersonID:   req.PersonID,
			Credential: req.Credential,
			Revision:   req.IdentityRevision,
		}); err != nil {
			return ConfirmResult{}, err
		}

		count := len(existing) + 1

		if threshold > 0 && count >= threshold {
			if err := c.Transition(ceremony.StatePendingSignature); err != nil {
				return ConfirmResult{}, err
			}
		}
		c.BumpRevision()
		if err := tx.SaveCeremony(c); err != nil {
			return ConfirmResult{}, err
		}

		return ConfirmResult{
			PersonID:      req.PersonID,
			WitnessCount:  count,
			Threshold:     threshold,
			QuorumReached: threshold > 0 && count >= threshold,
			State:         c.State,
			Revision:      c.Revision,
		}, nil
	})
}
