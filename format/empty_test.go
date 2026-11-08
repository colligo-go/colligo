package format

import "testing"

func TestEmptyTextFileIsInvalidForEveryFormat(t *testing.T) {
	for label, reason := range map[string]string{
		ContentFastq: "no records",
		ContentFasta: "no records",
		ContentGTF:   "no feature lines",
	} {
		s := structureOf(t, extract(t, validatorFor(t, label), "", 1))
		if s.Valid || s.Reason != reason || s.Offset != 0 {
			t.Errorf("%s: valid %v reason %q offset %d, want invalid %q at 0", label, s.Valid, s.Reason, s.Offset, reason)
		}
	}
}
