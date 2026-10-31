package text

import (
	"path/filepath"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/render/theme"
	"github.com/colligo-go/colligo/verifier"
)

func (v *view) result(f *verifier.FileReport) (string, theme.Role) {
	switch f.Status {
	case verifier.StatusUnreadable:
		return v.theme.Failed + " unreadable", theme.Bad
	case verifier.StatusInvalid:
		text := v.theme.Failed + " invalid"
		if onlyRunFailure(f) {
			text += " (its run failed)"
		}
		return text, theme.Bad
	}
	if f.Status == verifier.StatusQuick {
		return v.theme.Advisory + " quick (not read in full)", theme.Warn
	}
	if f.Worst() == model.Advisory {
		return v.theme.Advisory + " " + f.Status + " (with a note)", theme.Warn
	}
	return v.theme.Verified + " " + f.Status, theme.Good
}

func onlyRunFailure(f *verifier.FileReport) bool {
	seen := false
	for _, a := range f.Assertions {
		if !failed(a) || a.Severity.Rank() < model.Blocking.Rank() {
			continue
		}
		if a.ID != diagnoses.SourceRunFailed {
			return false
		}
		seen = true
	}
	return seen
}

func baseOf(entry string) string { return filepath.Base(entry) }
