package verifier

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/colligo-go/colligo/facts"
	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/identity"
	"github.com/colligo-go/colligo/internal/fsaccess"
	"github.com/colligo-go/colligo/secret"
)

const (
	IdentityIdentified    = "identified"
	IdentityNotIdentified = "not_identified"

	IdentifySchemaVersion = 1
)

// Identification is the answer of identify for one file. A file that
// matches no known reference is not an error: it is not_identified, and the
// digest, when there is one, is still reported.
type Identification struct {
	Name      secret.Name           `json:"path"`
	Status    string                `json:"status"`
	Content   string                `json:"content,omitempty"`
	Size      int64                 `json:"size"`
	Seqcol    *identity.SeqcolValue `json:"seqcol,omitempty"`
	Reference *identity.Reference   `json:"reference,omitempty"`
	Reason    string                `json:"reason,omitempty"`
}

func (i *Identification) String() string { return fmt.Sprintf("%s: %s", i.Name, i.Status) }

// IdentifyFile reads the file once. It returns an error only for
// cancellation; every other failure is the Status of the answer, with the
// Reason.
func IdentifyFile(ctx context.Context, name secret.Name, references []identity.Reference) (*Identification, error) {
	answer := &Identification{Name: name}
	f, err := fsaccess.Open(name.Raw)
	if err != nil {
		return unreadableIdentity(answer, err), nil
	}
	defer f.Close()
	return identifySource(ctx, answer, f, references)
}

func identifySource(ctx context.Context, answer *Identification, f readSource, references []identity.Reference) (*Identification, error) {
	info, err := f.Stat()
	if err != nil {
		return unreadableIdentity(answer, statFailure(err)), nil
	}
	before := stateOf(info)
	res, err := facts.Compute(ctx, f, facts.Options{Identify: true})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return unreadableIdentity(answer, err), nil
	}
	observed, err := observedChange(answer.Name, f, before)
	if err != nil {
		return unreadableIdentity(answer, err), nil
	}
	if observed != "" {
		return unreadableIdentity(answer, fmt.Errorf("the file changed while it was being read (%s); run identify again", observed)), nil
	}
	answer.Content, answer.Size = res.Content, res.Size
	return resolveIdentity(answer, res, references), nil
}

func resolveIdentity(answer *Identification, res facts.Result, references []identity.Reference) *Identification {
	answer.Status = IdentityNotIdentified
	fact, ok := res.Fact(identity.FactKind)
	if !ok {
		answer.Reason = unidentifiableReason(res)
		return answer
	}
	var value identity.SeqcolValue
	if err := json.Unmarshal(fact.Value, &value); err != nil {
		return unreadableIdentity(answer, err)
	}
	if value.Digest == "" {
		answer.Reason = value.Reason
		return answer
	}
	answer.Seqcol = &value
	if ref, ok := identity.Recognize(value.Digest, references); ok {
		answer.Status, answer.Reference = IdentityIdentified, &ref
		return answer
	}
	answer.Reason = "the sequence collection matches no known reference"
	return answer
}

func unidentifiableReason(res facts.Result) string {
	container, content := structureFacts(res)
	for _, s := range []*structureFact{container, content} {
		if s != nil && !s.Valid {
			return fmt.Sprintf("the %s is not valid: %s (at byte %d)", s.Format, s.Reason, s.Offset)
		}
	}
	if res.Content == format.ContentFasta {
		return "the FASTA has no sequence collection"
	}
	return fmt.Sprintf("a %s file has no identity to derive; only FASTA can be identified", res.Content)
}

func unreadableIdentity(answer *Identification, err error) *Identification {
	answer.Status = StatusUnreadable
	answer.Reason = err.Error()
	return answer
}

// IdentifyAll identifies each file in order, one at a time.
func IdentifyAll(ctx context.Context, names []secret.Name, references []identity.Reference) (IdentifyReport, error) {
	report := IdentifyReport{}
	for _, name := range names {
		answer, err := IdentifyFile(ctx, name, references)
		if err != nil {
			return IdentifyReport{}, err
		}
		report.Files = append(report.Files, answer)
	}
	return report, nil
}

type IdentifyReport struct {
	Files []*Identification `json:"files"`
}

func (r IdentifyReport) ExitCode() int {
	for _, f := range r.Files {
		if f.Status == StatusUnreadable {
			return ExitFatal
		}
	}
	return ExitOK
}

type jsonIdentify struct {
	SchemaVersion int               `json:"schema_version"`
	Files         []*Identification `json:"files"`
	ExitCode      int               `json:"exit_code"`
}

func (r IdentifyReport) WriteJSON(w io.Writer) error {
	files := r.Files
	if files == nil {
		files = []*Identification{}
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonIdentify{SchemaVersion: IdentifySchemaVersion, Files: files, ExitCode: r.ExitCode()})
}

func (r IdentifyReport) WriteText(w io.Writer) {
	for _, f := range r.Files {
		fmt.Fprintf(w, "%s\n", f.Name)
		switch f.Status {
		case IdentityIdentified:
			fmt.Fprintf(w, "  identified: %s\n  seqcol: %s (%d sequences)\n  source: %s\n", f.Reference.Label, f.Seqcol.Digest, f.Seqcol.Sequences, f.Reference.Source)
		case IdentityNotIdentified:
			fmt.Fprintf(w, "  not identified: %s\n", f.Reason)
			if f.Seqcol != nil {
				fmt.Fprintf(w, "  seqcol: %s (%d sequences)\n", f.Seqcol.Digest, f.Seqcol.Sequences)
			}
		default:
			fmt.Fprintf(w, "  %s: %s\n", f.Status, f.Reason)
		}
	}
}
