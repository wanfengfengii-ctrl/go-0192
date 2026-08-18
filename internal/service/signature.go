package service

import (
	"context"
	"fmt"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/hsm"
	"quorumforge/internal/store"
)

// BeginRequest opens the single HSM signing session for a ceremony.
type BeginRequest struct {
	CeremonyID ceremony.ID       `json:"ceremony_id"`
	Operation  string            `json:"operation"`
	Revision   ceremony.Revision `json:"revision"`
	Token      string            `json:"token"`
	SessionID  string            `json:"session_id"`
}

// BeginResult is returned after a signing session is opened.
type BeginResult struct {
	SessionID  string              `json:"session_id"`
	Revision   ceremony.Revision   `json:"revision"`
	Digest     ceremony.Digest     `json:"digest"`
	KeyVersion ceremony.KeyVersion `json:"key_version"`
	State      ceremony.State      `json:"state"`
}

// BeginSignature consumes a one-time token and opens exactly one HSM session
// bound to the current revision, request digest and key version.
func (s *Service) BeginSignature(ctx context.Context, req BeginRequest) (BeginResult, error) {
	if req.CeremonyID == "" || req.Operation == "" || req.Token == "" {
		return BeginResult{}, fmt.Errorf("%w: missing required fields", ErrInvalidArgument)
	}
	sessionID := req.SessionID
	if sessionID == "" {
		sessionID = newID()
	}

	content := contentOf(req.Token, sessionID)

	return runCommand(s, ctx, commandSpec{
		id:        req.CeremonyID,
		operation: req.Operation,
		kind:      "begin-signature",
		content:   content,
	}, func(tx store.Tx) (BeginResult, error) {
		c, err := tx.LoadCeremony(ctx, req.CeremonyID)
		if err != nil {
			return BeginResult{}, err
		}
		if c.State != ceremony.StatePendingSignature {
			return BeginResult{}, fmt.Errorf("%w: state %s", ErrQuorumNotReached, c.State)
		}
		if err := c.GuardRevision(req.Revision); err != nil {
			return BeginResult{}, err
		}

		if _, err := tx.LoadSession(ctx, req.CeremonyID); err == nil {
			return BeginResult{}, ErrSessionExists
		} else if err != store.ErrNotFound {
			return BeginResult{}, err
		}

		// Validate the same binding that the HSM enforces before consuming the
		// durable one-time token. The store remains the source of truth for
		// replay protection across process restarts.
		if err := hsm.ValidateSession(hsm.Session{
			ID:         hsm.SessionID(sessionID),
			Revision:   uint64(c.Revision),
			Digest:     string(c.RequestDigest),
			KeyVersion: string(c.KeyVersion),
			Token:      hsm.Token(req.Token),
		}); err != nil {
			return BeginResult{}, fmt.Errorf("%w: %v", ErrInvalidArgument, err)
		}

		consumed, err := tx.ConsumeToken(ctx, req.CeremonyID, req.Token)
		if err != nil {
			return BeginResult{}, err
		}
		if consumed {
			return BeginResult{}, ErrTokenReused
		}

		if err := tx.SaveSession(req.CeremonyID, store.SessionRecord{
			ID:         sessionID,
			Revision:   uint64(c.Revision),
			Digest:     string(c.RequestDigest),
			KeyVersion: string(c.KeyVersion),
		}); err != nil {
			return BeginResult{}, err
		}

		c.BumpRevision()
		if err := tx.SaveCeremony(c); err != nil {
			return BeginResult{}, err
		}

		return BeginResult{
			SessionID:  sessionID,
			Revision:   c.Revision,
			Digest:     c.RequestDigest,
			KeyVersion: c.KeyVersion,
			State:      c.State,
		}, nil
	})
}

// ReceiptRequest records the HSM acknowledgement on the open session.
type ReceiptRequest struct {
	CeremonyID ceremony.ID       `json:"ceremony_id"`
	Operation  string            `json:"operation"`
	Revision   ceremony.Revision `json:"revision"`
	SessionID  string            `json:"session_id"`
	Receipt    string            `json:"receipt"`
}

// ReceiptResult is returned after recording an HSM receipt.
type ReceiptResult struct {
	SessionID string            `json:"session_id"`
	Receipt   string            `json:"receipt"`
	State     ceremony.State    `json:"state"`
	Revision  ceremony.Revision `json:"revision"`
}

// RecordReceipt stores the HSM receipt on the unique signing session.
func (s *Service) RecordReceipt(ctx context.Context, req ReceiptRequest) (ReceiptResult, error) {
	if req.CeremonyID == "" || req.Operation == "" || req.SessionID == "" || req.Receipt == "" {
		return ReceiptResult{}, fmt.Errorf("%w: missing required fields", ErrInvalidArgument)
	}

	content := contentOf(req.SessionID, req.Receipt)

	return runCommand(s, ctx, commandSpec{
		id:        req.CeremonyID,
		operation: req.Operation,
		kind:      "record-receipt",
		content:   content,
	}, func(tx store.Tx) (ReceiptResult, error) {
		c, err := tx.LoadCeremony(ctx, req.CeremonyID)
		if err != nil {
			return ReceiptResult{}, err
		}
		if c.State != ceremony.StatePendingSignature {
			return ReceiptResult{}, fmt.Errorf("receipt not allowed in state %s", c.State)
		}
		if err := c.GuardRevision(req.Revision); err != nil {
			return ReceiptResult{}, err
		}

		sess, err := tx.LoadSession(ctx, req.CeremonyID)
		if err == store.ErrNotFound {
			return ReceiptResult{}, ErrNoSession
		}
		if err != nil {
			return ReceiptResult{}, err
		}
		if sess.ID != req.SessionID {
			return ReceiptResult{}, fmt.Errorf("%w: session %q", ErrNoSession, req.SessionID)
		}

		if err := tx.RecordSessionReceipt(req.CeremonyID, req.Receipt); err != nil {
			return ReceiptResult{}, err
		}

		c.BumpRevision()
		if err := tx.SaveCeremony(c); err != nil {
			return ReceiptResult{}, err
		}

		return ReceiptResult{
			SessionID: req.SessionID,
			Receipt:   req.Receipt,
			State:     c.State,
			Revision:  c.Revision,
		}, nil
	})
}

// ArtifactRequest registers the single signature product for a ceremony.
type ArtifactRequest struct {
	CeremonyID ceremony.ID       `json:"ceremony_id"`
	Operation  string            `json:"operation"`
	Revision   ceremony.Revision `json:"revision"`
	Digest     string            `json:"digest"`
}

// ArtifactResult is returned after registering a signature product.
type ArtifactResult struct {
	Digest   string            `json:"digest"`
	State    ceremony.State    `json:"state"`
	Revision ceremony.Revision `json:"revision"`
}

// RegisterArtifact records the at-most-one signature product and advances the
// ceremony to pending review.
func (s *Service) RegisterArtifact(ctx context.Context, req ArtifactRequest) (ArtifactResult, error) {
	if req.CeremonyID == "" || req.Operation == "" || req.Digest == "" {
		return ArtifactResult{}, fmt.Errorf("%w: missing required fields", ErrInvalidArgument)
	}

	content := contentOf(req.Digest)

	return runCommand(s, ctx, commandSpec{
		id:        req.CeremonyID,
		operation: req.Operation,
		kind:      "register-artifact",
		content:   content,
	}, func(tx store.Tx) (ArtifactResult, error) {
		c, err := tx.LoadCeremony(ctx, req.CeremonyID)
		if err != nil {
			return ArtifactResult{}, err
		}
		if c.State != ceremony.StatePendingSignature {
			return ArtifactResult{}, fmt.Errorf("artifact registration not allowed in state %s", c.State)
		}
		if err := c.GuardRevision(req.Revision); err != nil {
			return ArtifactResult{}, err
		}

		sess, err := tx.LoadSession(ctx, req.CeremonyID)
		if err == store.ErrNotFound {
			return ArtifactResult{}, ErrNoSession
		}
		if err != nil {
			return ArtifactResult{}, err
		}
		if sess.Receipt == "" {
			return ArtifactResult{}, fmt.Errorf("artifact registration requires an HSM receipt")
		}

		if _, err := tx.LoadArtifact(ctx, req.CeremonyID); err == nil {
			return ArtifactResult{}, ErrArtifactExists
		} else if err != store.ErrNotFound {
			return ArtifactResult{}, err
		}

		if err := tx.SaveArtifact(req.CeremonyID, store.ArtifactRecord{Digest: req.Digest}); err != nil {
			return ArtifactResult{}, err
		}

		if err := c.Transition(ceremony.StatePendingReview); err != nil {
			return ArtifactResult{}, err
		}
		c.BumpRevision()
		if err := tx.SaveCeremony(c); err != nil {
			return ArtifactResult{}, err
		}

		return ArtifactResult{
			Digest:   req.Digest,
			State:    c.State,
			Revision: c.Revision,
		}, nil
	})
}
