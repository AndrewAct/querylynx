package llm

// RedactKey masks an API key for safe inclusion in logs or error messages.
// Only the first 4 characters are preserved; the rest become "***".
func RedactKey(key string) string {
	if len(key) <= 4 {
		return "***"
	}
	return key[:4] + "***"
}
