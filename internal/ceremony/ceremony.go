// Package ceremony defines the signature ceremony aggregate and its
// irreversible seven-state lifecycle.
package ceremony

import (
	"encoding/json"
	"fmt"
)

// State enumerates the lifecycle of a root key signature ceremony.
type State uint8

const (
	// StatePendingLock is the initial state before the request digest,
	// key version, policy version and participant scope are frozen.
	StatePendingLock State = iota
	// StateGatheringWitnesses accepts witness confirmations toward quorum.
	StateGatheringWitnesses
	// StatePendingSignature means quorum was reached and an HSM session
	// may be opened exactly once.
	StatePendingSignature
	// StatePendingReview holds the signature artifact awaiting review.
	StatePendingReview
	// StateSealed is a terminal state reached once quorum, review and a
	// single signature artifact all hold.
	StateSealed
	// StateQuarantined is the terminal isolation state.
	StateQuarantined
	// StateCancelled is the terminal cancellation state.
	StateCancelled
)

// String returns a stable human-readable state name.
func (s State) String() string {
	switch s {
	case StatePendingLock:
		return "pending-lock"
	case StateGatheringWitnesses:
		return "gathering-witnesses"
	case StatePendingSignature:
		return "pending-signature"
	case StatePendingReview:
		return "pending-review"
	case StateSealed:
		return "sealed"
	case StateQuarantined:
		return "quarantined"
	case StateCancelled:
		return "cancelled"
	default:
		return "unknown"
	}
}

// IsTerminal reports whether s is a final state.
func (s State) IsTerminal() bool {
	return s == StateSealed || s == StateQuarantined || s == StateCancelled
}

// MarshalJSON serialises the state as its stable human-readable name.
func (s State) MarshalJSON() ([]byte, error) {
	return json.Marshal(s.String())
}

// UnmarshalJSON parses a state name back into a State.
func (s *State) UnmarshalJSON(data []byte) error {
	var name string
	if err := json.Unmarshal(data, &name); err != nil {
		return err
	}
	switch name {
	case "pending-lock":
		*s = StatePendingLock
	case "gathering-witnesses":
		*s = StateGatheringWitnesses
	case "pending-signature":
		*s = StatePendingSignature
	case "pending-review":
		*s = StatePendingReview
	case "sealed":
		*s = StateSealed
	case "quarantined":
		*s = StateQuarantined
	case "cancelled":
		*s = StateCancelled
	default:
		return fmt.Errorf("unknown state %q", name)
	}
	return nil
}

// ID uniquely identifies a ceremony.
type ID string

// Revision is a monotonically increasing ceremony revision number.
type Revision uint64

// Operation is an idempotency key supplied by callers.
type Operation string

// Digest is a frozen cryptographic request digest.
type Digest string

// KeyVersion identifies a root key version.
type KeyVersion string

// PolicyVersion identifies a policy catalog version.
type PolicyVersion string

// Ceremony is the aggregate root for a single root key signature request.
type Ceremony struct {
	ID            ID
	State         State
	Revision      Revision
	RequestDigest Digest
	KeyVersion    KeyVersion
	PolicyVersion PolicyVersion
	// ParticipantScope is the frozen set of participant natural-person IDs.
	ParticipantScope []string
	// ReviewConclusion records the artifact review outcome once produced.
	ReviewConclusion ReviewConclusion
	// ReviewDigest is the review digest captured during review.
	ReviewDigest string
	// TerminalReason records why a terminal state was reached.
	TerminalReason string
}

// Lock freezes the request digest, key version, policy version and
// participant scope. Locking is irreversible: the resulting snapshot is the
// only one later commands may reference.
func (c *Ceremony) Lock(digest Digest, key KeyVersion, policy PolicyVersion, participants []string) error {
	if c.State != StatePendingLock {
		return fmt.Errorf("%w: current state %s", ErrNotLockable, c.State)
	}
	if digest == "" || key == "" || policy == "" {
		return fmt.Errorf("%w: digest, key version and policy version are required", ErrInvalidSnapshot)
	}
	if len(participants) == 0 {
		return fmt.Errorf("%w: participant scope must not be empty", ErrInvalidSnapshot)
	}
	c.RequestDigest = digest
	c.KeyVersion = key
	c.PolicyVersion = policy
	c.ParticipantScope = append([]string(nil), participants...)
	c.State = StateGatheringWitnesses
	return nil
}

// Snapshot is the immutable locked view of a ceremony.
type Snapshot struct {
	Digest           Digest
	KeyVersion       KeyVersion
	PolicyVersion    PolicyVersion
	ParticipantScope []string
}

// Snapshot returns the frozen snapshot, valid only after locking.
func (c *Ceremony) Snapshot() (Snapshot, error) {
	if c.State == StatePendingLock {
		return Snapshot{}, fmt.Errorf("%w: ceremony not locked", ErrNotLocked)
	}
	return Snapshot{
		Digest:           c.RequestDigest,
		KeyVersion:       c.KeyVersion,
		PolicyVersion:    c.PolicyVersion,
		ParticipantScope: append([]string(nil), c.ParticipantScope...),
	}, nil
}

// ReviewConclusion records the outcome of the artifact review.
type ReviewConclusion string

// Well-known review conclusions.
const (
	ReviewPending  ReviewConclusion = "pending"
	ReviewApproved ReviewConclusion = "approved"
	ReviewRejected ReviewConclusion = "rejected"
)

// Valid reports whether the conclusion is one of the recognized values.
func (c ReviewConclusion) Valid() bool {
	switch c {
	case ReviewPending, ReviewApproved, ReviewRejected:
		return true
	default:
		return false
	}
}

// Approved reports whether the review concluded positively.
func (c ReviewConclusion) Approved() bool { return c == ReviewApproved }
