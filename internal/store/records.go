package store

import (
	"time"

	"quorumforge/internal/ceremony"
)

// WitnessRecord is a persisted witness confirmation.
type WitnessRecord struct {
	PersonID   string
	Credential string
	Revision   uint64
}

// ScopeEntry is a frozen participant identity captured at lock time.
type ScopeEntry struct {
	PersonID string
	Role     string
	Revision uint64
}

// SessionRecord is a persisted HSM signing session.
type SessionRecord struct {
	ID         string
	Revision   uint64
	Digest     string
	KeyVersion string
	Receipt    string
}

// ArtifactRecord is a persisted signature product.
type ArtifactRecord struct {
	Digest       string
	ReviewDigest string
}

// OperationRecord is an idempotency entry for a single ceremony command.
type OperationRecord struct {
	CeremonyID ceremony.ID
	Operation  string
	Kind       string
	Content    string
	Result     string
	CreatedAt  string
}

// AuditEvent is an append-only audit entry.
type AuditEvent struct {
	CeremonyID ceremony.ID
	Operation  string
	Kind       string
	Outcome    string
	Detail     string
	OccurredAt time.Time
}

// Aggregate is the fully reconstructed ceremony state, including its children.
type Aggregate struct {
	Ceremony  ceremony.Ceremony
	Scope     []ScopeEntry
	Witnesses []WitnessRecord
	Session   *SessionRecord
	Artifact  *ArtifactRecord
}
