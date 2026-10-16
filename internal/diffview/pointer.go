package diffview

// Ptr returns a pointer to text for callers constructing a Change directly.
// Call and At do not need it; it is useful to describe an in-memory change.
func Ptr(text string) *string { return &text }
