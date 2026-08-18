package sqlite

// schema is the full DDL applied on open. All tables use IF NOT EXISTS so that
// reopening an existing database is idempotent.
const schema = `
CREATE TABLE IF NOT EXISTS policies (
	version             TEXT PRIMARY KEY,
	threshold           INTEGER NOT NULL,
	allowed_key_version TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS policy_roles (
	policy_version TEXT NOT NULL,
	role           TEXT NOT NULL,
	PRIMARY KEY (policy_version, role)
);

CREATE TABLE IF NOT EXISTS participants (
	person_id TEXT PRIMARY KEY,
	role      TEXT NOT NULL,
	revision  INTEGER NOT NULL,
	revoked   INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS participant_credentials (
	person_id  TEXT NOT NULL,
	credential TEXT NOT NULL,
	PRIMARY KEY (person_id, credential)
);

CREATE TABLE IF NOT EXISTS ceremonies (
	id                TEXT PRIMARY KEY,
	state             INTEGER NOT NULL,
	revision          INTEGER NOT NULL,
	request_digest    TEXT NOT NULL,
	key_version       TEXT NOT NULL,
	policy_version    TEXT NOT NULL,
	review_conclusion TEXT NOT NULL DEFAULT 'pending',
	review_digest     TEXT NOT NULL DEFAULT '',
	terminal_reason   TEXT NOT NULL DEFAULT '',
	created_at        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS ceremony_scope (
	ceremony_id TEXT NOT NULL,
	person_id   TEXT NOT NULL,
	role        TEXT NOT NULL,
	revision    INTEGER NOT NULL,
	PRIMARY KEY (ceremony_id, person_id)
);

CREATE TABLE IF NOT EXISTS witnesses (
	ceremony_id TEXT NOT NULL,
	person_id   TEXT NOT NULL,
	credential  TEXT NOT NULL,
	revision    INTEGER NOT NULL,
	PRIMARY KEY (ceremony_id, person_id)
);

CREATE TABLE IF NOT EXISTS sessions (
	ceremony_id TEXT PRIMARY KEY,
	session_id  TEXT NOT NULL,
	revision    INTEGER NOT NULL,
	digest      TEXT NOT NULL,
	key_version TEXT NOT NULL,
	receipt     TEXT NOT NULL DEFAULT ''
);

CREATE TABLE IF NOT EXISTS consumed_tokens (
	token       TEXT PRIMARY KEY,
	ceremony_id TEXT NOT NULL,
	consumed_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS artifacts (
	ceremony_id   TEXT PRIMARY KEY,
	digest        TEXT NOT NULL,
	review_digest TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS operations (
	ceremony_id TEXT NOT NULL,
	operation   TEXT NOT NULL,
	kind        TEXT NOT NULL,
	content     TEXT NOT NULL,
	result      TEXT NOT NULL,
	created_at  TEXT NOT NULL,
	PRIMARY KEY (ceremony_id, operation)
);

CREATE TABLE IF NOT EXISTS audit_events (
	id          INTEGER PRIMARY KEY AUTOINCREMENT,
	ceremony_id TEXT NOT NULL,
	operation   TEXT NOT NULL,
	kind        TEXT NOT NULL,
	outcome     TEXT NOT NULL,
	detail      TEXT NOT NULL,
	occurred_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_audit_ceremony ON audit_events (ceremony_id);
CREATE INDEX IF NOT EXISTS idx_operations_ceremony ON operations (ceremony_id);
`
