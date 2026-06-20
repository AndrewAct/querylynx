// Package prompt renders a dialect-neutral schema into compact text for LLM
// prompts. Shared by the table router (rag) and the text2sql agent so both see
// the schema described the same way.
package prompt

import (
	"fmt"
	"strings"

	"querylynx/internal/schema"
)

// Schema renders tables with row counts, columns (type, nullability, FK target,
// sample values). Sample values are the highest-signal part: they let the model
// route and write WHERE clauses against real enum-like values instead of guessing.
func Schema(s schema.Schema) string {
	var b strings.Builder
	for _, t := range s.Tables {
		fmt.Fprintf(&b, "Table %s (%d rows):\n", t.Name, t.RowCount)
		fkByCol := make(map[string]schema.ForeignKey, len(t.ForeignKeys))
		for _, fk := range t.ForeignKeys {
			fkByCol[fk.Column] = fk
		}
		for _, c := range t.Columns {
			line := fmt.Sprintf("  - %s %s", c.Name, c.DataType)
			if c.Nullable {
				line += " NULL"
			}
			if fk, ok := fkByCol[c.Name]; ok {
				line += fmt.Sprintf(" -> %s.%s", fk.RefTable, fk.RefColumn)
			}
			if len(c.SampleValues) > 0 {
				line += " [e.g. " + strings.Join(c.SampleValues, ", ") + "]"
			}
			b.WriteString(line + "\n")
		}
	}
	return b.String()
}

// Subset returns a copy of s containing only the named tables (schema order
// preserved). Unknown names are ignored.
func Subset(s schema.Schema, names []string) schema.Schema {
	keep := make(map[string]bool, len(names))
	for _, n := range names {
		keep[n] = true
	}
	out := schema.Schema{DatabaseName: s.DatabaseName}
	for _, t := range s.Tables {
		if keep[t.Name] {
			out.Tables = append(out.Tables, t)
		}
	}
	return out
}
