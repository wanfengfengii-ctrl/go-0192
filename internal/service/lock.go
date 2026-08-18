package service

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/policy"
	"quorumforge/internal/store"
)

// LockRequest freezes the request digest, key version, policy version and the
// participant scope of a ceremony.
type LockRequest struct {
	CeremonyID    ceremony.ID            `json:"ceremony_id"`
	Operation     string                 `json:"operation"`
	Revision      ceremony.Revision      `json:"revision"`
	Digest        ceremony.Digest        `json:"digest"`
	KeyVersion    ceremony.KeyVersion    `json:"key_version"`
	PolicyVersion ceremony.PolicyVersion `json:"policy_version"`
	Participants  []string               `json:"participants"`
}

// LockResult is returned after a successful lock.
type LockResult struct {
	ID            ceremony.ID            `json:"id"`
	State         ceremony.State         `json:"state"`
	Revision      ceremony.Revision      `json:"revision"`
	Digest        ceremony.Digest        `json:"digest"`
	KeyVersion    ceremony.KeyVersion    `json:"key_version"`
	PolicyVersion ceremony.PolicyVersion `json:"policy_version"`
	Participants  []string               `json:"participants"`
	Threshold     int                    `json:"threshold"`
}

// normalizeParticipants returns a sorted, de-duplicated participant list.
func normalizeParticipants(in []string) ([]string, error) {
	if len(in) == 0 {
		return nil, fmt.Errorf("%w: participant scope must not be empty", ErrInvalidArgument)
	}
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, p := range in {
		p = strings.TrimSpace(p)
		if p == "" {
			return nil, fmt.Errorf("%w: empty participant id", ErrInvalidArgument)
		}
		if seen[p] {
			return nil, fmt.Errorf("%w: duplicate participant %q", ErrInvalidArgument, p)
		}
		seen[p] = true
		out = append(out, p)
	}
	sort.Strings(out)
	return out, nil
}

// Lock validates the snapshot against the policy and participant directory,
// then freezes the ceremony and its participant scope atomically.
func (s *Service) Lock(ctx context.Context, req LockRequest) (LockResult, error) {
	if req.CeremonyID == "" || req.Operation == "" {
		return LockResult{}, fmt.Errorf("%w: missing ceremony id or operation", ErrInvalidArgument)
	}
	if req.Digest == "" || req.KeyVersion == "" || req.PolicyVersion == "" {
		return LockResult{}, fmt.Errorf("%w: digest, key version and policy version are required", ErrInvalidArgument)
	}

	participants, err := normalizeParticipants(req.Participants)
	if err != nil {
		return LockResult{}, err
	}

	// Resolve and validate the policy and participant identities up front.
	pol, err := s.store.GetPolicy(ctx, policy.Version(req.PolicyVersion))
	if err != nil {
		if err == store.ErrNotFound {
			return LockResult{}, fmt.Errorf("%w: %s", ErrInvalidArgument, req.PolicyVersion)
		}
		return LockResult{}, err
	}
	if !pol.AllowsKeyVersion(policy.KeyVersion(req.KeyVersion)) {
		return LockResult{}, fmt.Errorf("%w: policy %s allows %s", ErrInvalidArgument, req.PolicyVersion, pol.AllowedKeyVersion)
	}

	scope := make([]store.ScopeEntry, 0, len(participants))
	for _, pid := range participants {
		part, err := s.store.GetParticipant(ctx, policy.PersonID(pid))
		if err != nil {
			if err == store.ErrNotFound {
				return LockResult{}, fmt.Errorf("%w: participant %s", ErrIdentityMismatch, pid)
			}
			return LockResult{}, err
		}
		if !part.Active() {
			return LockResult{}, fmt.Errorf("%w: %s", ErrRevoked, pid)
		}
		if !pol.AllowsRole(part.Role) {
			return LockResult{}, fmt.Errorf("%w: %s for policy %s", ErrRoleNotAllowed, part.Role, req.PolicyVersion)
		}
		scope = append(scope, store.ScopeEntry{
			PersonID: pid,
			Role:     string(part.Role),
			Revision: part.Revision,
		})
	}

	content := contentOf(
		string(req.Digest),
		string(req.KeyVersion),
		string(req.PolicyVersion),
		strings.Join(participants, ","),
	)

	return runCommand(s, ctx, commandSpec{
		id:        req.CeremonyID,
		operation: req.Operation,
		kind:      "lock",
		content:   content,
	}, func(tx store.Tx) (LockResult, error) {
		c, err := tx.LoadCeremony(ctx, req.CeremonyID)
		if err != nil {
			return LockResult{}, err
		}
		if err := c.GuardRevision(req.Revision); err != nil {
			return LockResult{}, err
		}
		if err := c.Lock(req.Digest, req.KeyVersion, req.PolicyVersion, participants); err != nil {
			return LockResult{}, err
		}
		c.BumpRevision()
		if err := tx.SaveScope(req.CeremonyID, scope); err != nil {
			return LockResult{}, err
		}
		if err := tx.SaveCeremony(c); err != nil {
			return LockResult{}, err
		}
		return LockResult{
			ID:            c.ID,
			State:         c.State,
			Revision:      c.Revision,
			Digest:        c.RequestDigest,
			KeyVersion:    c.KeyVersion,
			PolicyVersion: c.PolicyVersion,
			Participants:  append([]string(nil), c.ParticipantScope...),
			Threshold:     pol.Threshold,
		}, nil
	})
}
