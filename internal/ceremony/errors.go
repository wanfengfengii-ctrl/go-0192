package ceremony

import "errors"

// Sentinel errors shared across the domain.
var (
	ErrNotLocked          = errors.New("ceremony not locked")
	ErrNotLockable        = errors.New("ceremony not lockable")
	ErrInvalidSnapshot    = errors.New("invalid lock snapshot")
	ErrStaleRevision      = errors.New("stale revision")
	ErrTerminal           = errors.New("ceremony already in terminal state")
	ErrIllegalTransition  = errors.New("illegal state transition")
	ErrOperationConflict  = errors.New("operation content conflict")
	ErrQuorumNotReached   = errors.New("quorum not reached")
	ErrSessionRequired    = errors.New("signing session required")
	ErrArtifactRequired   = errors.New("signature artifact required")
	ErrReviewRequired     = errors.New("review conclusion required")
	ErrDigestMismatch     = errors.New("request digest mismatch")
	ErrKeyVersionMismatch = errors.New("key version mismatch")
	ErrPolicyMismatch     = errors.New("policy version mismatch")
)
