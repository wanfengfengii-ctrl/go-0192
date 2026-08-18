package witness

import "errors"

// ErrInvalidConfirmation is returned for a structurally invalid confirmation.
var ErrInvalidConfirmation = errors.New("invalid witness confirmation")

// NewLedgerFrom reconstructs a ledger from persisted confirmations, ignoring
// any entry that is stale or malformed so that recovery never panics.
func NewLedgerFrom(confirmations []Confirmation) *Ledger {
	l := NewLedger()
	for _, c := range confirmations {
		if err := validate(c); err != nil {
			continue
		}
		// Best-effort: Record already rejects duplicates, which is the correct
		// behaviour when the persisted set is internally inconsistent.
		_ = l.Record(c)
	}
	return l
}

// Contains reports whether the natural person has already confirmed.
func (l *Ledger) Contains(id PersonID) bool {
	_, ok := l.seen[id]
	return ok
}

// ConfirmationFor returns the stored confirmation for a person, if present.
func (l *Ledger) ConfirmationFor(id PersonID) (Confirmation, bool) {
	c, ok := l.seen[id]
	return c, ok
}

// validate enforces the structural invariants of a confirmation.
func validate(c Confirmation) error {
	if c.PersonID == "" || c.Credential == "" {
		return ErrInvalidConfirmation
	}
	if c.Revision == 0 {
		return ErrStaleConfirmation
	}
	return nil
}
