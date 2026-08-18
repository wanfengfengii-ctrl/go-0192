package sqlite

import (
	"context"
	"database/sql"

	"quorumforge/internal/ceremony"
	"quorumforge/internal/store"
)

// queryer abstracts the subset of database/sql shared by *sql.DB and *sql.Tx so
// that load helpers are reusable inside and outside transactions.
type queryer interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// loadSession reads the single session for a ceremony, returning ErrNotFound
// when none exists.
func loadSession(ctx context.Context, q queryer, id ceremony.ID) (*store.SessionRecord, error) {
	var rec store.SessionRecord
	err := q.QueryRowContext(ctx,
		`SELECT session_id, revision, digest, key_version, receipt FROM sessions WHERE ceremony_id=?`, id).
		Scan(&rec.ID, &rec.Revision, &rec.Digest, &rec.KeyVersion, &rec.Receipt)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// loadArtifact reads the single signature product for a ceremony.
func loadArtifact(ctx context.Context, q queryer, id ceremony.ID) (*store.ArtifactRecord, error) {
	var rec store.ArtifactRecord
	err := q.QueryRowContext(ctx,
		`SELECT digest, review_digest FROM artifacts WHERE ceremony_id=?`, id).
		Scan(&rec.Digest, &rec.ReviewDigest)
	if err == sql.ErrNoRows {
		return nil, store.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

// LoadSession is the store-level session reader.
func (s *Store) LoadSession(ctx context.Context, id ceremony.ID) (*store.SessionRecord, error) {
	return loadSession(ctx, s.db, id)
}

// LoadArtifact is the store-level artifact reader.
func (s *Store) LoadArtifact(ctx context.Context, id ceremony.ID) (*store.ArtifactRecord, error) {
	return loadArtifact(ctx, s.db, id)
}
