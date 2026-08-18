package sqlite

import (
	"context"
	"database/sql"
	"time"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/store"
)

// txImpl adapts a *sql.Tx to the store.Tx interface and injects faults after
// selected writes so that rollback behaviour can be tested.
type txImpl struct {
	tx     *sql.Tx
	faults *FaultInjector
}

func (t *txImpl) failIf(point string) error {
	if t.faults == nil {
		return nil
	}
	return t.faults.Fail(point)
}

func (t *txImpl) LoadCeremony(ctx context.Context, id ceremony.ID) (ceremony.Ceremony, error) {
	var c ceremony.Ceremony
	err := t.tx.QueryRowContext(ctx,
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

func (t *txImpl) SaveCeremony(c ceremony.Ceremony) error {
	res, err := t.tx.ExecContext(context.Background(),
		`UPDATE ceremonies SET state=?, revision=?, request_digest=?, key_version=?, policy_version=?, review_conclusion=?, review_digest=?, terminal_reason=?
		 WHERE id=?`,
		c.State, c.Revision, c.RequestDigest, c.KeyVersion, c.PolicyVersion,
		c.ReviewConclusion, c.ReviewDigest, c.TerminalReason, c.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return t.failIf(FaultAfterSaveCeremony)
}

func (t *txImpl) SaveScope(id ceremony.ID, entries []store.ScopeEntry) error {
	for _, e := range entries {
		if _, err := t.tx.ExecContext(context.Background(),
			`INSERT INTO ceremony_scope (ceremony_id, person_id, role, revision) VALUES (?, ?, ?, ?)
			 ON CONFLICT(ceremony_id, person_id) DO UPDATE SET role=excluded.role, revision=excluded.revision`,
			id, e.PersonID, e.Role, e.Revision); err != nil {
			return err
		}
	}
	return t.failIf(FaultAfterSaveScope)
}

func (t *txImpl) LoadScope(ctx context.Context, id ceremony.ID) ([]store.ScopeEntry, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT person_id, role, revision FROM ceremony_scope WHERE ceremony_id=? ORDER BY person_id`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.ScopeEntry
	for rows.Next() {
		var e store.ScopeEntry
		if err := rows.Scan(&e.PersonID, &e.Role, &e.Revision); err != nil {
			return nil, err
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

func (t *txImpl) SaveWitness(id ceremony.ID, w store.WitnessRecord) error {
	if _, err := t.tx.ExecContext(context.Background(),
		`INSERT INTO witnesses (ceremony_id, person_id, credential, revision) VALUES (?, ?, ?, ?)
		 ON CONFLICT(ceremony_id, person_id) DO UPDATE SET credential=excluded.credential, revision=excluded.revision`,
		id, w.PersonID, w.Credential, w.Revision); err != nil {
		return err
	}
	return t.failIf(FaultAfterSaveWitness)
}

func (t *txImpl) LoadWitnesses(ctx context.Context, id ceremony.ID) ([]store.WitnessRecord, error) {
	rows, err := t.tx.QueryContext(ctx, `SELECT person_id, credential, revision FROM witnesses WHERE ceremony_id=?`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []store.WitnessRecord
	for rows.Next() {
		var w store.WitnessRecord
		if err := rows.Scan(&w.PersonID, &w.Credential, &w.Revision); err != nil {
			return nil, err
		}
		out = append(out, w)
	}
	return out, rows.Err()
}

func (t *txImpl) SaveSession(id ceremony.ID, s store.SessionRecord) error {
	if _, err := t.tx.ExecContext(context.Background(),
		`INSERT INTO sessions (ceremony_id, session_id, revision, digest, key_version, receipt) VALUES (?, ?, ?, ?, ?, ?)`,
		id, s.ID, s.Revision, s.Digest, s.KeyVersion, s.Receipt); err != nil {
		if isUniqueViolation(err) {
			return store.ErrDuplicate
		}
		return err
	}
	return t.failIf(FaultAfterSaveSession)
}

func (t *txImpl) RecordSessionReceipt(id ceremony.ID, receipt string) error {
	res, err := t.tx.ExecContext(context.Background(),
		`UPDATE sessions SET receipt=? WHERE ceremony_id=?`, receipt, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return store.ErrNotFound
	}
	return t.failIf(FaultAfterSaveSession)
}

func (t *txImpl) LoadSession(ctx context.Context, id ceremony.ID) (*store.SessionRecord, error) {
	return loadSession(ctx, t.tx, id)
}

func (t *txImpl) SaveArtifact(id ceremony.ID, a store.ArtifactRecord) error {
	if _, err := t.tx.ExecContext(context.Background(),
		`INSERT INTO artifacts (ceremony_id, digest, review_digest) VALUES (?, ?, ?)
		 ON CONFLICT(ceremony_id) DO UPDATE SET digest=excluded.digest, review_digest=excluded.review_digest`,
		id, a.Digest, a.ReviewDigest); err != nil {
		return err
	}
	return t.failIf(FaultAfterSaveArtifact)
}

func (t *txImpl) LoadArtifact(ctx context.Context, id ceremony.ID) (*store.ArtifactRecord, error) {
	return loadArtifact(ctx, t.tx, id)
}

func (t *txImpl) ConsumeToken(ctx context.Context, ceremonyID ceremony.ID, token string) (bool, error) {
	var one int
	err := t.tx.QueryRowContext(ctx, `SELECT 1 FROM consumed_tokens WHERE token = ?`, token).Scan(&one)
	if err == nil {
		return true, nil
	}
	if err != sql.ErrNoRows {
		return false, err
	}
	if _, err := t.tx.ExecContext(ctx,
		`INSERT INTO consumed_tokens (token, ceremony_id, consumed_at) VALUES (?, ?, datetime('now'))`,
		token, ceremonyID); err != nil {
		if isUniqueViolation(err) {
			return true, nil
		}
		return false, err
	}
	if err := t.failIf(FaultAfterConsumeToken); err != nil {
		return false, err
	}
	return false, nil
}

func (t *txImpl) LoadOperation(ctx context.Context, id ceremony.ID, op string) (*store.OperationRecord, error) {
	var rec store.OperationRecord
	err := t.tx.QueryRowContext(ctx,
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

func (t *txImpl) SaveOperation(rec store.OperationRecord) error {
	if _, err := t.tx.ExecContext(context.Background(),
		`INSERT INTO operations (ceremony_id, operation, kind, content, result, created_at) VALUES (?, ?, ?, ?, ?, datetime('now'))
		 ON CONFLICT(ceremony_id, operation) DO UPDATE SET kind=excluded.kind, content=excluded.content, result=excluded.result`,
		rec.CeremonyID, rec.Operation, rec.Kind, rec.Content, rec.Result); err != nil {
		return err
	}
	return t.failIf(FaultAfterSaveOperation)
}

func (t *txImpl) SaveAudit(e store.AuditEvent) error {
	if e.OccurredAt.IsZero() {
		e.OccurredAt = time.Now().UTC()
	}
	_, err := t.tx.ExecContext(context.Background(),
		`INSERT INTO audit_events (ceremony_id, operation, kind, outcome, detail, occurred_at) VALUES (?, ?, ?, ?, ?, ?)`,
		e.CeremonyID, e.Operation, e.Kind, e.Outcome, e.Detail, e.OccurredAt.Format(time.RFC3339))
	return err
}
