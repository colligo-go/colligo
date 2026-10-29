package secret

// Secret wraps a credential. Never exposes the real value through printing
// or serialization.
type Secret struct{ v string }

func (Secret) String() string               { return "[REDACTED]" }
func (Secret) GoString() string             { return "[REDACTED]" }
func (Secret) MarshalJSON() ([]byte, error) { return []byte(`"[REDACTED]"`), nil }

func (s Secret) reveal() string { return s.v }
