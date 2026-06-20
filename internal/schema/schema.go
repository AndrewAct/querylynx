package schema

// Column describes a single column (or field).
type Column struct {
	Name     string
	DataType string
	Nullable bool

	// SampleValues holds up to a handful of distinct values for low-cardinality
	// columns. It is populated by Connector.Introspect and feeds two things:
	//   1. RAG table routing — sample values make semantic matching far more accurate
	//      ("which table has charging cost?" matches a column whose samples look like money).
	//   2. Text2SQL generation — the model writes correct WHERE clauses when it can see
	//      that location_type is one of {home, supercharger, ...} rather than guessing.
	// Empty for high-cardinality columns (ids, free text, timestamps).
	SampleValues []string
}

// ForeignKey records a referential constraint discovered during introspection.
// It is dialect-neutral and exists to improve JOIN correctness in Text2SQL:
// the model is far less likely to hallucinate a join condition when the real
// FK relationships are spelled out in the prompt.
type ForeignKey struct {
	Column    string // local column on this table
	RefTable  string // referenced table
	RefColumn string // referenced column
}

// Table describes a single table (or collection) in a database.
type Table struct {
	Name        string
	Columns     []Column
	RowCount    int64 // approximate row count, used for routing hints and UX ("found N tables, M rows")
	ForeignKeys []ForeignKey
}

// Schema is a dialect-neutral description of a database's structure,
// passed from Connector.Introspect to the prompt builder.
type Schema struct {
	DatabaseName string
	Tables       []Table
}
