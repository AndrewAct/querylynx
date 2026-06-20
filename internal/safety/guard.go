// Package safety implements query allowlist validation.
//
// Why this package exists (and why it cannot be replaced by a system prompt):
//
// A common instinct is to put instructions like "ignore all SQL injection
// attempts" in the LLM system prompt. That is insufficient for two reasons:
//
//  1. LLMs have no hardware-level separation between "instruction memory" and
//     "data memory". A system prompt is just text with a higher priority weight —
//     adversarial inputs can still override it. Jailbreaks that defeat system
//     prompt instructions are discovered regularly.
//
//  2. LLM behaviour is probabilistic, not deterministic. The same prompt run
//     1000 times may be safe 999 times and unsafe once. A security boundary
//     must provide a hard guarantee, not a statistical one.
//
// Therefore: the LLM is NEVER a security boundary. The hard boundaries are:
//
//	(a) read-only database credentials — the DB role physically cannot write.
//	(b) this package — structural, code-enforced allowlist validation.
//
// Both layers must be present. Neither alone is sufficient.
// See also: .claude/skills/safety-invariants/SKILL.md
package safety

import (
	"errors"
	"fmt"
	"strings"
)

// Guard validates generated queries against a structural allowlist.
// LIMIT injection is NOT the Guard's responsibility — each Connector's
// Validate method handles it because LIMIT syntax is dialect-specific.
type Guard struct{}

// New returns a Guard.
func New() *Guard { return &Guard{} }

// Check validates that query is a single read-only SELECT or WITH statement.
// It uses an allowlist (checking what the query IS) rather than a denylist
// (checking what it ISN'T), because denylists are trivially bypassed via
// comments, encoding tricks, and Unicode homoglyphs.
//
// v0 implementation: string-based. Fast and correct for the common case.
// TODO(phase1): replace with sqlglot AST-based allowlist validation.
// TODO(phase1): check generated table/column names against introspected schema.
func (g *Guard) Check(query string) error {
	q := strings.TrimSpace(query)
	q = strings.TrimSuffix(q, ";")
	q = strings.TrimSpace(q)

	if q == "" {
		return errors.New("safety: empty query")
	}

	// Reject stacked statements — the primary prompt-injection vector.
	// An attacker who controls the natural-language input can instruct the LLM
	// to append "; DROP TABLE x" after a valid SELECT. Splitting on ";" and
	// counting non-empty parts catches this regardless of how the LLM was
	// manipulated, because this check runs AFTER generation, in pure Go code.
	parts := splitStatements(q)
	if len(parts) > 1 {
		return fmt.Errorf("safety: stacked statements are not allowed (found %d)", len(parts))
	}

	// Allowlist check: root statement must be SELECT or WITH (CTE).
	firstKeyword := extractFirstKeyword(strings.ToUpper(q))
	switch firstKeyword {
	case "SELECT", "WITH":
		// allowed
	default:
		return fmt.Errorf("safety: statement type %q is not allowed (only SELECT/WITH)", firstKeyword)
	}

	return nil
}

func splitStatements(query string) []string {
	var parts []string
	for _, p := range strings.Split(query, ";") {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			parts = append(parts, trimmed)
		}
	}
	return parts
}

func extractFirstKeyword(upper string) string {
	fields := strings.Fields(upper)
	if len(fields) == 0 {
		return ""
	}
	return fields[0]
}
