package identity

// Reference is a known Sequence Collection. Source records where the digest
// was checked: an entry without a source does not belong in the table.
type Reference struct {
	Label  string `json:"name"`
	Digest string `json:"digest"`
	Source string `json:"source"`
}

// Known returns the embedded table. Every entry must have been checked
// against an official seqcol or refget server, with the origin in Source.
func Known() []Reference {
	return nil
}

// Recognize returns the reference whose digest equals the given one.
func Recognize(digest string, references []Reference) (Reference, bool) {
	for _, r := range references {
		if r.Digest == digest {
			return r, true
		}
	}
	return Reference{}, false
}
