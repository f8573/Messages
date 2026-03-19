package sqlutil

// Nullable converts empty string to nil for SQL NULL value handling
func Nullable(v string) any {
	if v == "" {
		return nil
	}
	return v
}
