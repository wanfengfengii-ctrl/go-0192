// Package sqlite implements the store.Repository boundary on top of a SQLite
// database in WAL mode. It provides transactional command execution, unique
// index enforcement and fault injection for rollback/recovery testing.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/policy"
	"quorumforge/internal/store"
)

// Store is a SQLite-backed store implementation.
type Store struct {
	db     *sql.DB
	faults *FaultInjector
}

// Open opens (or creates) the SQLite database at path, enables WAL mode and
// applies the schema. An empty path selects an in-memory database.
func Open(path string) (*Store, error) {
	dsn := dsnFor(path)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	// A single writer connection avoids SQLITE_BUSY and keeps the single-node
	// service deterministic. WAL still permits concurrent readers.
	db.SetMaxOpenConns(1)

	if err := db.Ping(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping sqlite: %w", err)
	}
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}
	return &Store{db: db, faults: NewFaultInjector()}, nil
}

// dsnFor builds the connection string, enabling WAL and foreign keys for file
// databases and a busy timeout everywhere.
func dsnFor(path string) string {
	if path == "" || path == ":memory:" {
		return "file::memory:?_pragma=busy_timeout(5000)&_pragma=foreign_keys(ON)"
	}
	return "file:" + path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=synchronous(NORMAL)"
}

// Close releases the underlying handle.
func (s *Store) Close() error {
	return s.db.Close()
}

// InjectFault arms a fault at the named point.
func (s *Store) InjectFault(point string, err error) {
	s.faults.Set(point, err)
}

// ClearFaults disarms every fault.
func (s *Store) ClearFaults() {
	s.faults.Clear()
}

// Within runs fn inside a transaction, committing only when fn returns nil. Any
// error, including an injected fault, rolls the transaction back.
func (s *Store) Within(ctx context.Context, fn func(store.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	inner := &txImpl{tx: tx, faults: s.faults}
	if err := fn(inner); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := inner.failIf(FaultBeforeCommit); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tx: %w", err)
	}
	return nil
}

func (s *Store) exec(ctx context.Context, query string, args ...any) (sql.Result, error) {
	return s.db.ExecContext(ctx, query, args...)
}

func (s *Store) query(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return s.db.QueryContext(ctx, query, args...)
}

// UpsertPolicy persists a policy version and its allowed roles.
func (s *Store) UpsertPolicy(ctx context.Context, p policy.Policy) error {
	if _, err := s.exec(ctx,
		`INSERT INTO policies (version, threshold, allowed_key_version) VALUES (?, ?, ?)
		 ON CONFLICT(version) DO UPDATE SET threshold=excluded.threshold, allowed_key_version=excluded.allowed_key_version`,
		p.Version, p.Threshold, p.AllowedKeyVersion); err != nil {
		return err
	}
	if _, err := s.exec(ctx, `DELETE FROM policy_roles WHERE policy_version = ?`, p.Version); err != nil {
		return err
	}
	for _, r := range p.AllowedRoles {
		if _, err := s.exec(ctx,
			`INSERT INTO policy_roles (policy_version, role) VALUES (?, ?)`,
			p.Version, r); err != nil {
			return err
		}
	}
	return nil
}

// GetPolicy loads a policy version.
func (s *Store) GetPolicy(ctx context.Context, v policy.Version) (policy.Policy, error) {
	var p policy.Policy
	err := s.db.QueryRowContext(ctx,
		`SELECT version, threshold, allowed_key_version FROM policies WHERE version = ?`, v).
		Scan(&p.Version, &p.Threshold, &p.AllowedKeyVersion)
	if err == sql.ErrNoRows {
		return policy.Policy{}, store.ErrNotFound
	}
	if err != nil {
		return policy.Policy{}, err
	}
	rows, err := s.query(ctx, `SELECT role FROM policy_roles WHERE policy_version = ? ORDER BY role`, v)
	if err != nil {
		return policy.Policy{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var r policy.Role
		if err := rows.Scan(&r); err != nil {
			return policy.Policy{}, err
		}
		p.AllowedRoles = append(p.AllowedRoles, r)
	}
	return p, rows.Err()
}

// UpsertParticipant persists a participant identity and its credentials.
func (s *Store) UpsertParticipant(ctx context.Context, p policy.Participant) error {
	revoked := 0
	if p.Revoked {
		revoked = 1
	}
	if _, err := s.exec(ctx,
		`INSERT INTO participants (person_id, role, revision, revoked) VALUES (?, ?, ?, ?)
		 ON CONFLICT(person_id) DO UPDATE SET role=excluded.role, revision=excluded.revision, revoked=excluded.revoked`,
		p.PersonID, p.Role, p.Revision, revoked); err != nil {
		return err
	}
	if _, err := s.exec(ctx, `DELETE FROM participant_credentials WHERE person_id = ?`, p.PersonID); err != nil {
		return err
	}
	for _, c := range p.Credentials {
		if _, err := s.exec(ctx,
			`INSERT INTO participant_credentials (person_id, credential) VALUES (?, ?)`,
			p.PersonID, c); err != nil {
			return err
		}
	}
	return nil
}

// GetParticipant loads a participant identity.
func (s *Store) GetParticipant(ctx context.Context, id policy.PersonID) (policy.Participant, error) {
	var p policy.Participant
	var revoked int
	err := s.db.QueryRowContext(ctx,
		`SELECT person_id, role, revision, revoked FROM participants WHERE person_id = ?`, id).
		Scan(&p.PersonID, &p.Role, &p.Revision, &revoked)
	if err == sql.ErrNoRows {
		return policy.Participant{}, store.ErrNotFound
	}
	if err != nil {
		return policy.Participant{}, err
	}
	p.Revoked = revoked != 0
	rows, err := s.query(ctx, `SELECT credential FROM participant_credentials WHERE person_id = ? ORDER BY credential`, id)
	if err != nil {
		return policy.Participant{}, err
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			return policy.Participant{}, err
		}
		p.Credentials = append(p.Credentials, c)
	}
	return p, rows.Err()
}

// ListParticipants returns all participants sorted by person id.
func (s *Store) ListParticipants(ctx context.Context) ([]policy.Participant, error) {
	rows, err := s.query(ctx, `SELECT person_id FROM participants ORDER BY person_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]policy.Participant, 0, len(ids))
	for _, id := range ids {
		p, err := s.GetParticipant(ctx, policy.PersonID(id))
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// CreateCeremony persists a new ceremony aggregate.
func (s *Store) CreateCeremony(ctx context.Context, c ceremony.Ceremony) error {
	_, err := s.exec(ctx,
		`INSERT INTO ceremonies (id, state, revision, request_digest, key_version, policy_version, review_conclusion, review_digest, terminal_reason, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, datetime('now'))`,
		c.ID, c.State, c.Revision, c.RequestDigest, c.KeyVersion, c.PolicyVersion,
		c.ReviewConclusion, c.ReviewDigest, c.TerminalReason)
	if err != nil {
		if isUniqueViolation(err) {
			return store.ErrDuplicate
		}
		return err
	}
	return nil
}

// LoadCeremony loads the ceremony aggregate row only.
func (s *Store) LoadCeremony(ctx context.Context, id ceremony.ID) (ceremony.Ceremony, error) {
	var c ceremony.Ceremony
	err := s.db.QueryRowContext(ctx,
		`SELECT id, state, revision, request_digest, key_version, policy_version, review_conclusion, review_digest, terminal_reason
		 FROM ceremonies WHERE id = ?`, id).
		Scan(&c.ID, &c.State, &c.Revision, &c.RequestDigest, &c.KeyVersion, &c.PolicyVersion,
			&c.ReviewConclusion, &c.ReviewDigest, &c.TerminalReason)
	if err == sql.ErrNoRows {
		return ceremony.Ceremony{}, store.ErrNotFound
	}
	if err != nil {
		return ceremony.Ceremony{}, err
	}
	return c, nil
}

// SaveCeremony updates the aggregate row.
func (s *Store) SaveCeremony(ctx context.Context, c ceremony.Ceremony) error {
	res, err := s.exec(ctx,
		`UPDATE ceremonies SET state=?, revision=?, request_digest=?, key_version=?, policy_version=?, review_conclusion=?, review_digest=?, terminal_reason=?
		 WHERE id=?`,
		c.State, c.Revision, c.RequestDigest, c.KeyVersion, c.PolicyVersion,
		c.ReviewConclusion, c.ReviewDigest, c.TerminalReason, c.ID)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return store.ErrNotFound
	}
	return nil
}

// LoadOperation loads an idempotency record for a ceremony command.
func (s *Store) LoadOperation(ctx context.Context, id ceremony.ID, op string) (*store.OperationRecord, error) {
	var rec store.OperationRecord
	err := s.db.QueryRowContext(ctx,
		`SELECT ceremony_id, operation, kind, content, result, created_at FROM operations WHERE ceremony_id=? AND operation=?`,
		id, op).Scan(&rec.CeremonyID, &rec.Operation, &rec.Kind, &rec.Content, &rec.Result, &rec.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// AppendAudit writes an audit event outside a ceremony transaction.
func (s *Store) AppendAudit(ctx context.Context, e store.AuditEvent) error {
	_, err := s.exec(ctx,
		`INSERT INTO audit_events (ceremony_id, operation, kind, outcome, detail, occurred_at) VALUES (?, ?, ?, ?, ?, datetime('now'))`,
		e.CeremonyID, e.Operation, e.Kind, e.Outcome, e.Detail)
	return err
}

// LoadAggregate reconstructs the full ceremony state, including its scope,
// witnesses, session and artifact.
func (s *Store) LoadAggregate(ctx context.Context, id ceremony.ID) (*store.Aggregate, error) {
	c, err := s.LoadCeremony(ctx, id)
	if err != nil {
		return nil, err
	}
	agg := &store.Aggregate{Ceremony: c}

	scopeRows, err := s.query(ctx, `SELECT person_id, role, revision FROM ceremony_scope WHERE ceremony_id=? ORDER BY person_id`, id)
	if err != nil {
		return nil, err
	}
	for scopeRows.Next() {
		var e store.ScopeEntry
		if err := scopeRows.Scan(&e.PersonID, &e.Role, &e.Revision); err != nil {
			scopeRows.Close()
			return nil, err
		}
		agg.Scope = append(agg.Scope, e)
		agg.Ceremony.ParticipantScope = append(agg.Ceremony.ParticipantScope, e.PersonID)
	}
	if err := scopeRows.Err(); err != nil {
		scopeRows.Close()
		return nil, err
	}
	scopeRows.Close()

	wRows, err := s.query(ctx, `SELECT person_id, credential, revision FROM witnesses WHERE ceremony_id=?`, id)
	if err != nil {
		return nil, err
	}
	for wRows.Next() {
		var w store.WitnessRecord
		if err := wRows.Scan(&w.PersonID, &w.Credential, &w.Revision); err != nil {
			wRows.Close()
			return nil, err
		}
		agg.Witnesses = append(agg.Witnesses, w)
	}
	if err := wRows.Err(); err != nil {
		wRows.Close()
		return nil, err
	}
	wRows.Close()

	sess, err := s.LoadSession(ctx, id)
	if err != nil && err != store.ErrNotFound {
		return nil, err
	}
	if err == nil {
		agg.Session = sess
	}

	art, err := s.LoadArtifact(ctx, id)
	if err != nil && err != store.ErrNotFound {
		return nil, err
	}
	if err == nil {
		agg.Artifact = art
	}

	return agg, nil
}
