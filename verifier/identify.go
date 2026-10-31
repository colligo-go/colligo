package verifier

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/colligo-go/colligo/facts"
	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/identity"
	"github.com/colligo-go/colligo/internal/fsaccess"
	"github.com/colligo-go/colligo/secret"
)

const (
	IdentityIdentified    = "identified"
	IdentityDeclared      = "declared"
	IdentityConflict      = "conflict"
	IdentityNotIdentified = "not_identified"

	IdentifySchemaVersion = 1
)

// Identification is the answer of identify for one file. A file that
// matches no known reference is not an error: it is not_identified, and the
// digest, when there is one, is still reported.
//
// identified means the content was matched against something official: a
// sequence collection or the checksum its source published. declared means
// only that the file says what it is, in a GENCODE header, which anyone can
// edit. conflict means the file says it is GENCODE and its content says
// otherwise.
type Identification struct {
	Name      secret.Name           `json:"path"`
	Status    string                `json:"status"`
	Content   string                `json:"content,omitempty"`
	Size      int64                 `json:"size"`
	Seqcol    *identity.SeqcolValue `json:"seqcol,omitempty"`
	Reference *identity.Reference   `json:"reference,omitempty"`
	Checksum  *identity.Checksum    `json:"checksum,omitempty"`
	Gencode   *identity.Gencode     `json:"gencode,omitempty"`
	Reason    string                `json:"reason,omitempty"`
}

func (i *Identification) String() string { return fmt.Sprintf("%s: %s", i.Name, i.Status) }

// IdentifyFile reads the file once. It returns an error only for
// cancellation; every other failure is the Status of the answer, with the
// Reason.
func IdentifyFile(ctx context.Context, name secret.Name, table identity.Table) (*Identification, error) {
	answer := &Identification{Name: name}
	f, err := fsaccess.Open(name.Raw)
	if err != nil {
		return unreadableIdentity(answer, err), nil
	}
	defer f.Close()
	return identifySource(ctx, answer, f, table)
}

func identifySource(ctx context.Context, answer *Identification, f readSource, table identity.Table) (*Identification, error) {
	info, err := f.Stat()
	if err != nil {
		return unreadableIdentity(answer, statFailure(err)), nil
	}
	before := stateOf(info)
	res, err := facts.Compute(ctx, f, facts.Options{Identify: true, MD5: true})
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
	return resolveIdentity(answer, res, table), nil
}

func resolveIdentity(answer *Identification, res facts.Result, table identity.Table) *Identification {
	answer.Status = IdentityNotIdentified
	if reason := invalidStructure(res); reason != "" {
		answer.Status, answer.Reason = StatusInvalid, reason
		return answer
	}
	if sum, ok := identity.RecognizeChecksum(res.MD5Hex(), table.Checksums); ok {
		answer.Checksum = &sum
	}
	switch res.Content {
	case format.ContentFasta:
		if err := resolveSeqcol(answer, res, table); err != nil {
			return unreadableIdentity(answer, err)
		}
	case format.ContentGTF:
		if err := resolveGencode(answer, res); err != nil {
			return unreadableIdentity(answer, err)
		}
	default:
		answer.Reason = fmt.Sprintf("a %s file has no identity to derive from its content; only FASTA and GTF can be identified", res.Content)
	}
	if answer.Checksum != nil {
		answer.Status, answer.Reason = IdentityIdentified, ""
		return answer
	}
	if answer.Reference != nil {
		answer.Status, answer.Reason = IdentityIdentified, ""
	}
	return answer
}

func invalidStructure(res facts.Result) string {
	container, content := structureFacts(res)
	for _, s := range []*structureFact{container, content} {
		if s != nil && !s.Valid {
			return fmt.Sprintf("the %s is not valid: %s (at byte %d)", s.Format, s.Reason, s.Offset)
		}
	}
	return ""
}

func resolveSeqcol(answer *Identification, res facts.Result, table identity.Table) error {
	fact, ok := res.Fact(identity.FactKind)
	if !ok {
		answer.Reason = "the FASTA has no sequence collection"
		return nil
	}
	var value identity.SeqcolValue
	if err := json.Unmarshal(fact.Value, &value); err != nil {
		return err
	}
	if value.Digest == "" {
		answer.Reason = value.Reason
		return nil
	}
	answer.Seqcol = &value
	if ref, ok := identity.Recognize(value.Digest, table.Collections); ok {
		answer.Reference = &ref
		return nil
	}
	answer.Reason = "the sequence collection matches no known reference"
	return nil
}

func resolveGencode(answer *Identification, res facts.Result) error {
	fact, ok := res.Fact(identity.GTFHeaderKind)
	if !ok {
		answer.Reason = "the GTF has no header facts"
		return nil
	}
	var header identity.GTFHeader
	if err := json.Unmarshal(fact.Value, &header); err != nil {
		return err
	}
	gencode, ok := identity.ParseGencode(header)
	if !ok {
		answer.Reason = noGencodeReason(header)
		return nil
	}
	answer.Gencode = &gencode
	if contradictions := gencodeContradictions(res, header); len(contradictions) > 0 {
		answer.Status = IdentityConflict
		answer.Reason = fmt.Sprintf("the header says %s, but %s", gencode.Label, strings.Join(contradictions, "; "))
		return nil
	}
	answer.Status = IdentityDeclared
	answer.Reason = fmt.Sprintf("the header declares %s, and a header can be edited: no official checksum matches this file", gencode.Label)
	return nil
}

func noGencodeReason(header identity.GTFHeader) string {
	switch {
	case identity.NamesGencodeProvider(header):
		return "the header names GENCODE as the provider, but its ##description line carries no release"
	case header.Truncated:
		return fmt.Sprintf("the header is longer than the %d lines or %d bytes that are read, and no GENCODE release was found in them", identity.MaxHeaderLines, identity.MaxHeaderBytes)
	}
	return "the GTF header does not name a GENCODE release"
}

func gencodeContradictions(res facts.Result, header identity.GTFHeader) []string {
	var out []string
	switch sequenceConvention(res) {
	case "ensembl":
		out = append(out, "the sequence names follow the Ensembl convention (1, X, MT), and GENCODE names start with chr")
	case "mixed":
		out = append(out, "the sequence names mix the chr convention with the Ensembl one (1, X, MT), and GENCODE names all start with chr")
	}
	if header.GeneBiotypeLines > 0 {
		out = append(out, fmt.Sprintf("%d gene lines carry the Ensembl attribute gene_biotype, which GENCODE writes as gene_type", header.GeneBiotypeLines))
	}
	return out
}

func sequenceConvention(res facts.Result) string {
	fact, ok := res.Fact("seq_names")
	if !ok {
		return ""
	}
	var names struct {
		Convention string `json:"convention"`
	}
	if json.Unmarshal(fact.Value, &names) != nil {
		return ""
	}
	return names.Convention
}

func unreadableIdentity(answer *Identification, err error) *Identification {
	answer.Status = StatusUnreadable
	answer.Reason = err.Error()
	return answer
}

// IdentifyAll identifies each file in order, one at a time.
func IdentifyAll(ctx context.Context, names []secret.Name, table identity.Table) (IdentifyReport, error) {
	start := time.Now()
	report := IdentifyReport{}
	for _, name := range names {
		answer, err := IdentifyFile(ctx, name, table)
		if err != nil {
			return IdentifyReport{}, err
		}
		report.Files = append(report.Files, answer)
	}
	report.Elapsed = time.Since(start)
	return report, nil
}

type IdentifyReport struct {
	Files   []*Identification `json:"files"`
	Elapsed time.Duration     `json:"-"`
}

// ExitCode is 2 when a file could not be read, 3 when a file is not a valid
// instance of its format or its content contradicts what it declares, and 0
// when every file was answered, identified or not.
func (r IdentifyReport) ExitCode() int {
	code := ExitOK
	for _, f := range r.Files {
		switch f.Status {
		case StatusUnreadable:
			return ExitFatal
		case StatusInvalid, IdentityConflict:
			code = ExitIncomplete
		}
	}
	return code
}
