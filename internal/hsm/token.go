package hsm

import "errors"

// ErrInvalidSession is returned for a structurally invalid signing session.
var ErrInvalidSession = errors.New("invalid signing session")

// ErrInvalidToken is returned for an empty or malformed signing token.
var ErrInvalidToken = errors.New("invalid signing token")

// ValidateSession enforces the structural invariants of a signing session: it
// must bind a non-empty revision, request digest and key version.
func ValidateSession(s Session) error {
	if s.ID == "" || s.Digest == "" || s.KeyVersion == "" {
		return ErrInvalidSession
	}
	if s.Revision == 0 {
		return ErrInvalidSession
	}
	if s.Token == "" {
		return ErrInvalidToken
	}
	return nil
}

// HasSession reports whether a session has been opened.
func (r *Registry) HasSession() bool {
	return r.session != nil
}

// Session returns the open session, or nil.
func (r *Registry) Session() *Session {
	return r.session
}

// Receipt returns the recorded receipt for the open session.
func (r *Registry) Receipt() Receipt {
	if r.session == nil {
		return ""
	}
	return r.session.Receipt
}

// HasReceipt reports whether a receipt has been recorded.
func (r *Registry) HasReceipt() bool {
	return r.session != nil && r.session.Receipt != ""
}

// Artifact returns the registered signature product, or nil.
func (r *Registry) Artifact() *Artifact {
	return r.artifact
}

// TokenConsumed reports whether a token has already been consumed.
func (r *Registry) TokenConsumed(t Token) bool {
	return r.consumed[t]
}
