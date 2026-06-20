package sql

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"querylynx/internal/schema"
)

var (
	// Captures the token right after FROM or JOIN (the table reference).
	fromJoinRe = regexp.MustCompile(`(?i)\b(?:from|join)\s+("?[\w.]+"?)`)
	// Captures CTE names: WITH name AS ( ... , name AS ( ...
	cteRe = regexp.MustCompile(`(?i)(?:\bwith\b|,)\s+([a-zA-Z_]\w*)\s+as\s*\(`)
)

// ValidateTables checks that every table referenced after FROM/JOIN exists in the
// schema (CTE names and subqueries are allowed). This is a cheap pre-execution
// hallucination check for the most common failure — a made-up table name.
//
// We deliberately do NOT string-parse column references here: without a real AST
// that produces false rejections (aliases, literals, function names). Column
// hallucinations are instead caught by the database itself ("column X does not
// exist"), whose error is the best possible self-correction feedback and is fed
// back into the loop by the pipeline. The DB is ground truth.
//
// TODO(phase2): replace with sqlglot AST table + column reference extraction.
func ValidateTables(query string, s schema.Schema) error {
	known := make(map[string]bool, len(s.Tables))
	for _, t := range s.Tables {
		known[strings.ToLower(t.Name)] = true
	}
	// CTE names defined in this query are valid references.
	for _, m := range cteRe.FindAllStringSubmatch(query, -1) {
		known[strings.ToLower(m[1])] = true
	}

	for _, m := range fromJoinRe.FindAllStringSubmatch(query, -1) {
		ref := m[1]
		if strings.HasPrefix(ref, "(") {
			continue // subquery, not a table name
		}
		name := normalizeTableRef(ref)
		if name == "" {
			continue
		}
		if !known[strings.ToLower(name)] {
			return fmt.Errorf("table %q does not exist; known tables: %s", name, knownList(s))
		}
	}
	return nil
}

// normalizeTableRef strips quotes and a schema qualifier: public."vehicles" -> vehicles.
func normalizeTableRef(ref string) string {
	ref = strings.Trim(ref, `"`)
	if i := strings.LastIndex(ref, "."); i >= 0 {
		ref = ref[i+1:]
	}
	return strings.Trim(ref, `"`)
}

func knownList(s schema.Schema) string {
	names := make([]string, 0, len(s.Tables))
	for _, t := range s.Tables {
		names = append(names, t.Name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}
