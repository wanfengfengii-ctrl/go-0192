package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/policy"
	"quorumforge/internal/store"
)

// Service is the application service that applies ceremony commands against a
// transactional store. It serializes command execution so that concurrent
// terminal transitions cannot both succeed.
type Service struct {
	store store.Store
	mu    sync.Mutex
}

// NewService constructs a service backed by the given store.
func NewService(s store.Store) *Service {
	return &Service{store: s}
}

// Store exposes the underlying store for seeding and diagnostics.
func (s *Service) Store() store.Store { return s.store }

// commandSpec identifies a ceremony command for idempotency tracking.
type commandSpec struct {
	id        ceremony.ID
	operation string
	kind      string
	content   string
}

// runCommand executes a single ceremony command inside one transaction,
// applying idempotency, optimistic revision checks and success auditing.
func runCommand[R any](s *Service, ctx context.Context, spec commandSpec, apply func(store.Tx) (R, error)) (R, error) {
	var zero R
	if spec.id == "" {
		return zero, fmt.Errorf("%w: missing ceremony id", ErrInvalidArgument)
	}
	if spec.operation == "" {
		return zero, fmt.Errorf("%w: missing operation key", ErrInvalidArgument)
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	var result R
	err := s.store.Within(ctx, func(tx store.Tx) error {
		existing, err := tx.LoadOperation(ctx, spec.id, spec.operation)
		switch {
		case err == nil:
			if existing.Content != spec.content {
				return fmt.Errorf("%w: operation %q kind %q", ErrOperationConflict, spec.operation, spec.kind)
			}
			if err := json.Unmarshal([]byte(existing.Result), &result); err != nil {
				return fmt.Errorf("decode stored result: %w", err)
			}
			return nil
		case err == store.ErrNotFound:
			// fall through to apply
		default:
			return err
		}

		r, err := apply(tx)
		if err != nil {
			return err
		}
		result = r

		data, err := json.Marshal(r)
		if err != nil {
			return fmt.Errorf("encode result: %w", err)
		}
		if err := tx.SaveOperation(store.OperationRecord{
			CeremonyID: spec.id,
			Operation:  spec.operation,
			Kind:       spec.kind,
			Content:    spec.content,
			Result:     string(data),
		}); err != nil {
			return err
		}
		return tx.SaveAudit(store.AuditEvent{
			CeremonyID: spec.id,
			Operation:  spec.operation,
			Kind:       spec.kind,
			Outcome:    "success",
		})
	})
	if err != nil {
		return zero, err
	}
	return result, nil
}

// CreateRequest carries the minimal fields needed to register a ceremony.
type CreateRequest struct {
	ID ceremony.ID `json:"id"`
}

// Create registers a new ceremony in the pending-lock state.
func (s *Service) Create(ctx context.Context, req CreateRequest) (ceremony.Ceremony, error) {
	if req.ID == "" {
		return ceremony.Ceremony{}, fmt.Errorf("%w: missing ceremony id", ErrInvalidArgument)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	c := ceremony.Ceremony{ID: req.ID, State: ceremony.StatePendingLock, ReviewConclusion: ceremony.ReviewPending}
	if err := s.store.CreateCeremony(ctx, c); err != nil {
		if err == store.ErrDuplicate {
			return ceremony.Ceremony{}, ErrAlreadyExists
		}
		return ceremony.Ceremony{}, err
	}
	return c, nil
}

// View is a read-only projection of a ceremony and its children.
type View struct {
	Ceremony      ceremony.Ceremony
	Scope         []store.ScopeEntry
	Witnesses     []store.WitnessRecord
	Session       *store.SessionRecord
	Artifact      *store.ArtifactRecord
	Threshold     int
	WitnessCount  int
	QuorumReached bool
}

// Get returns the current view of a ceremony.
func (s *Service) Get(ctx context.Context, id ceremony.ID) (View, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	agg, err := s.store.LoadAggregate(ctx, id)
	if err != nil {
		if err == store.ErrNotFound {
			return View{}, ErrNotFound
		}
		return View{}, err
	}
	v := View{
		Ceremony:     agg.Ceremony,
		Scope:        agg.Scope,
		Witnesses:    agg.Witnesses,
		Session:      agg.Session,
		Artifact:     agg.Artifact,
		WitnessCount: len(agg.Witnesses),
	}
	// Load the policy to surface the quorum threshold.
	if agg.Ceremony.PolicyVersion != "" {
		if p, err := s.store.GetPolicy(ctx, policy.Version(agg.Ceremony.PolicyVersion)); err == nil {
			v.Threshold = p.Threshold
		}
	}
	v.QuorumReached = v.Threshold > 0 && v.WitnessCount >= v.Threshold
	return v, nil
}

// newID generates a random hex identifier.
func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// contentOf builds a deterministic canonical content string for idempotency.
func contentOf(parts ...string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += "\x1f"
		}
		out += p
	}
	return out
}
