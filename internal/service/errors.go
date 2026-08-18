// Package service orchestrates the signature ceremony workflow by applying
// domain rules across the ceremony aggregate, witness ledger, HSM session
// registry and the transactional store.
package service

import "errors"

// Sentinel errors surfaced by the service layer.
var (
	ErrNotFound          = errors.New("ceremony not found")
	ErrAlreadyExists     = errors.New("ceremony already exists")
	ErrInvalidArgument   = errors.New("invalid argument")
	ErrOperationConflict = errors.New("operation content conflict")
	ErrNotLocked         = errors.New("ceremony not locked")
	ErrIdentityMismatch  = errors.New("participant identity revision mismatch")
	ErrRoleNotAllowed    = errors.New("participant role not allowed")
	ErrRevoked           = errors.New("participant revoked")
	ErrQuorumNotReached  = errors.New("quorum not reached")
	ErrDuplicateWitness  = errors.New("duplicate witness confirmation")
	ErrSessionExists     = errors.New("signing session already exists")
	ErrTokenReused       = errors.New("signing token already consumed")
	ErrArtifactExists    = errors.New("signature artifact already registered")
	ErrNoSession         = errors.New("no open signing session")
)
