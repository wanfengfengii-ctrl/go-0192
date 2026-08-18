package ceremony

import "fmt"

// allowed describes every legal state transition in the irreversible
// seven-state lifecycle. A transition from a terminal state is never legal.
var allowed = map[State]map[State]bool{
	StatePendingLock: {
		StateGatheringWitnesses: true,
		StateCancelled:          true,
	},
	StateGatheringWitnesses: {
		StatePendingSignature: true,
		StateQuarantined:      true,
		StateCancelled:        true,
	},
	StatePendingSignature: {
		StatePendingReview: true,
		StateQuarantined:   true,
		StateCancelled:     true,
	},
	StatePendingReview: {
		StateSealed:      true,
		StateQuarantined: true,
		StateCancelled:   true,
	},
	StateSealed:      {},
	StateQuarantined: {},
	StateCancelled:   {},
}

// CanTransition reports whether moving from the current state to next is legal.
func (c *Ceremony) CanTransition(next State) bool {
	return allowed[c.State][next]
}

// Transition advances the ceremony to next, returning an error when the move is
// illegal or the ceremony is already terminal.
func (c *Ceremony) Transition(next State) error {
	if c.State.IsTerminal() {
		return fmt.Errorf("%w: %s", ErrTerminal, c.State)
	}
	if !allowed[c.State][next] {
		return fmt.Errorf("%w: %s -> %s", ErrIllegalTransition, c.State, next)
	}
	c.State = next
	return nil
}

// GuardRevision rejects a command whose expected revision does not match the
// current revision, protecting the ledger from stale or replayed writes.
func (c *Ceremony) GuardRevision(expected Revision) error {
	if c.State == StatePendingLock {
		// Before locking, revision zero is the only acceptable expectation.
		if expected != 0 {
			return fmt.Errorf("%w: expected %d have %d", ErrStaleRevision, expected, c.Revision)
		}
		return nil
	}
	if expected != c.Revision {
		return fmt.Errorf("%w: expected %d have %d", ErrStaleRevision, expected, c.Revision)
	}
	return nil
}

// BumpRevision advances the revision counter after a successful command.
func (c *Ceremony) BumpRevision() {
	c.Revision++
}
