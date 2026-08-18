package sqlite

import "strings"

// isUniqueViolation reports whether err is a SQLite unique constraint failure.
// The pure-Go sqlite driver reports the SQLITE_CONSTRAINT error text directly,
// so matching the message is the most portable detection.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	s := err.Error()
	return strings.Contains(s, "UNIQUE constraint failed") ||
		strings.Contains(s, "constraint failed")
}
