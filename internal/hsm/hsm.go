// Package hsm models the single-use HSM signing session and the at-most-one
// signature artifact registry.
package hsm

import (
	"errors"
)

// Token is a one-time signing token consumed when a session opens.
type Token string

// SessionID uniquely identifies an HSM signing session.
type SessionID string

// Receipt is the acknowledgement returned by the HSM.
type Receipt string

// Artifact is the single signature product registered per ceremony.
type Artifact struct {
	Digest       string
	ReviewDigest string
}

// Session is the unique signing session bound to a ceremony revision,
// request digest and key version.
type Session struct {
	ID         SessionID
	Revision   uint64
	Digest     string
	KeyVersion string
	Token      Token
	Receipt    Receipt
	Consumed   bool
}

// Registry tracks the single signing session and at-most-one artifact.
type Registry struct {
	session  *Session
	artifact *Artifact
	consumed map[Token]bool
}

// NewRegistry returns an empty session and artifact registry.
func NewRegistry() *Registry {
	return &Registry{consumed: make(map[Token]bool)}
}

// OpenSession opens exactly one session for the ceremony. Repeated opens are
// rejected and a reused token is rejected even on the first distinct attempt.
func (r *Registry) OpenSession(s Session) error {
	if err := ValidateSession(s); err != nil {
		return err
	}
	if r.session != nil {
		return ErrSessionExists
	}
	if r.consumed[s.Token] {
		return ErrTokenReused
	}
	r.consumed[s.Token] = true
	s.Consumed = true
	r.session = &s
	return nil
}

// RecordReceipt stores the HSM receipt on the open session.
func (r *Registry) RecordReceipt(receipt Receipt) error {
	if r.session == nil {
		return ErrNoSession
	}
	r.session.Receipt = receipt
	return nil
}

// RegisterArtifact records at most one signature product.
func (r *Registry) RegisterArtifact(a Artifact) error {
	if r.artifact != nil {
		return ErrArtifactExists
	}
	r.artifact = &a
	return nil
}

// ArtifactCount returns the number of registered artifacts.
func (r *Registry) ArtifactCount() int {
	if r.artifact == nil {
		return 0
	}
	return 1
}

// Sentinel errors for session and artifact handling.
var (
	ErrSessionExists  = errors.New("signing session already exists")
	ErrTokenReused    = errors.New("signing token already consumed")
	ErrNoSession      = errors.New("no open signing session")
	ErrArtifactExists = errors.New("signature artifact already registered")
)
