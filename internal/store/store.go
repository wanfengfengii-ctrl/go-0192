// Package store declares the transactional repository boundaries for
// ceremonies, witnesses, sessions, artifacts, operations and audit events, as
// well as the persisted policy and participant directory.
package store

import (
	"context"
	"errors"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/policy"
)

// Store is the transactional persistence boundary. Implementations must
// guarantee that a failed transaction leaves no orphaned witness, session or
// artifact record, and that reopening the store after a restart reconstructs a
// consistent aggregate.
type Store interface {
	// Directory operations persist the policy catalog and participant registry.
	UpsertPolicy(ctx context.Context, p policy.Policy) error
	GetPolicy(ctx context.Context, v policy.Version) (policy.Policy, error)
	UpsertParticipant(ctx context.Context, p policy.Participant) error
	GetParticipant(ctx context.Context, id policy.PersonID) (policy.Participant, error)
	ListParticipants(ctx context.Context) ([]policy.Participant, error)

	// Ceremony operations persist the aggregate root.
	CreateCeremony(ctx context.Context, c ceremony.Ceremony) error
	LoadCeremony(ctx context.Context, id ceremony.ID) (ceremony.Ceremony, error)
	LoadAggregate(ctx context.Context, id ceremony.ID) (*Aggregate, error)
	SaveCeremony(ctx context.Context, c ceremony.Ceremony) error

	// Operation idempotency and audit.
	LoadOperation(ctx context.Context, id ceremony.ID, op string) (*OperationRecord, error)
	AppendAudit(ctx context.Context, e AuditEvent) error

	// Within runs fn inside a single transaction, committing only on success.
	Within(ctx context.Context, fn func(Tx) error) error

	// Fault injection for exercising rollback and recovery behaviour.
	InjectFault(point string, err error)
	ClearFaults()

	// Close releases the underlying database handle.
	Close() error
}

// Tx exposes transactional read/write operations for a single ceremony
// command. All writes made through a Tx are rolled back if the surrounding
// transaction returns an error.
type Tx interface {
	LoadCeremony(ctx context.Context, id ceremony.ID) (ceremony.Ceremony, error)
	SaveCeremony(c ceremony.Ceremony) error

	SaveScope(id ceremony.ID, entries []ScopeEntry) error
	LoadScope(ctx context.Context, id ceremony.ID) ([]ScopeEntry, error)

	SaveWitness(id ceremony.ID, w WitnessRecord) error
	LoadWitnesses(ctx context.Context, id ceremony.ID) ([]WitnessRecord, error)

	SaveSession(id ceremony.ID, s SessionRecord) error
	RecordSessionReceipt(id ceremony.ID, receipt string) error
	LoadSession(ctx context.Context, id ceremony.ID) (*SessionRecord, error)

	SaveArtifact(id ceremony.ID, a ArtifactRecord) error
	LoadArtifact(ctx context.Context, id ceremony.ID) (*ArtifactRecord, error)

	// ConsumeToken records a token as consumed. It returns (true, nil) when the
	// token was already consumed and (false, nil) on first consumption.
	ConsumeToken(ctx context.Context, ceremonyID ceremony.ID, token string) (alreadyConsumed bool, err error)

	LoadOperation(ctx context.Context, id ceremony.ID, op string) (*OperationRecord, error)
	SaveOperation(rec OperationRecord) error

	SaveAudit(e AuditEvent) error
}

// Sentinel errors for the repository boundary.
var (
	ErrNotFound      = errors.New("record not found")
	ErrConflict      = errors.New("concurrent conflict")
	ErrNotCommitted  = errors.New("transaction not committed")
	ErrDuplicate     = errors.New("duplicate record")
	ErrTokenConsumed = errors.New("token already consumed")
)
