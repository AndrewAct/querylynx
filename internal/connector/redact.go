package connector

import (
	"net/url"
	"regexp"
)

var kvPasswordRe = regexp.MustCompile(`(?i)(password=)[^ ]+`)

// RedactDSN strips credentials from a database DSN so it is safe to log or include
// in error messages. Handles both URI form and libpq key=value form:
//
//	postgres://user:pass@host:5432/db   -> postgres://user:***@host:5432/db
//	host=h user=u password=secret db=x  -> host=h user=u password=*** db=x
//
// Secrets must never appear in logs or traces (safety Invariant 6).
func RedactDSN(dsn string) string {
	if u, err := url.Parse(dsn); err == nil && u.Scheme != "" {
		if u.User != nil {
			if _, hasPass := u.User.Password(); hasPass {
				u.User = url.UserPassword(u.User.Username(), "***")
			}
		}
		return u.String()
	}
	return kvPasswordRe.ReplaceAllString(dsn, "${1}***")
}
