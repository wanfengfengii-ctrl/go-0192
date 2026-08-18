// Package witness records irrevocable witness confirmations and computes a
// stable quorum snapshot de-duplicated by natural person.
package witness

import (
	"errors"
	"sort"
)

// PersonID is a natural-person identifier used for de-duplication.
type PersonID string

// Credential is a concrete credential presented by a participant.
type Credential string

// Confirmation is a single witness's到场 confirmation.
type Confirmation struct {
	PersonID   PersonID
	Credential Credential
	Revision   uint64
}

// Ledger is the irrevocable witness confirmations collection.
type Ledger struct {
	// seen maps a natural person to their first accepted confirmation.
	seen map[PersonID]Confirmation
}

// NewLedger returns an empty witness ledger.
func NewLedger() *Ledger {
	return &Ledger{seen: make(map[PersonID]Confirmation)}
}

// Record accepts a confirmation if the natural person has not confirmed
// before. Duplicate or stale confirmations are rejected without mutating the
// ledger.
func (l *Ledger) Record(c Confirmation) error {
	if err := validate(c); err != nil {
		return err
	}
	if _, exists := l.seen[c.PersonID]; exists {
		return ErrDuplicateWitness
	}
	l.seen[c.PersonID] = c
	return nil
}

// Count returns the number of distinct natural-person witnesses.
func (l *Ledger) Count() int {
	return len(l.seen)
}

// Quorum returns a stable-sorted snapshot of accepted confirmations.
func (l *Ledger) Quorum() []Confirmation {
	out := make([]Confirmation, 0, len(l.seen))
	for _, c := range l.seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool {
		return string(out[i].PersonID) < string(out[j].PersonID)
	})
	return out
}

// Reached reports whether the distinct witness count meets threshold.
func (l *Ledger) Reached(threshold int) bool {
	return threshold > 0 && l.Count() >= threshold
}

// Sentinel errors for the witness ledger.
var (
	ErrDuplicateWitness  = errors.New("duplicate witness confirmation")
	ErrStaleConfirmation = errors.New("stale witness confirmation")
	ErrQuorumNotReached  = errors.New("quorum not reached")
)
