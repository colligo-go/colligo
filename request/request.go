// Package request turns whatever a person typed into one Request: the files
// to read, each with where it came from, and the source to compare with, if
// any. Every form of input is a small Resolver, so a new form is added
// without touching the other forms or the checks, which receive only the
// Request. Resolving reads names, never data: the one exception is the first
// bytes of a folder entry, read to tell data from clutter.
package request

import (
	"context"
	"fmt"
	"io"
	"slices"
	"strings"

	"github.com/colligo-go/colligo/internal/fsaccess"
	"github.com/colligo-go/colligo/samplesheet"
	"github.com/colligo-go/colligo/secret"
)

type OriginKind int

const (
	FromArgument OriginKind = iota
	FromFolder
	FromListLine
	FromSheetLine
)

// Origin says where a file came from. Line is 1-based and only set for the
// forms that have lines.
type Origin struct {
	Kind OriginKind
	Line int
}

func (o Origin) String() string {
	switch o.Kind {
	case FromFolder:
		return "folder"
	case FromListLine:
		return fmt.Sprintf("list line %d", o.Line)
	case FromSheetLine:
		return fmt.Sprintf("samplesheet line %d", o.Line)
	}
	return "argument"
}

// File is one file to read. A file named twice is one File with both origins.
type File struct {
	Name    secret.Name
	Origins []Origin
}

// Lines returns the lines of the given kind that named the file.
func (f File) Lines(kind OriginKind) []int {
	var lines []int
	for _, o := range f.Origins {
		if o.Kind == kind && !slices.Contains(lines, o.Line) {
			lines = append(lines, o.Line)
		}
	}
	return lines
}

// Provenance records how the accession of a SourceRef was obtained. Its value
// is what the JSON document and the text report show; Typed is empty because
// an accession typed exactly as it is used needs no explanation.
type Provenance string

const (
	Typed      Provenance = ""
	Normalized Provenance = "normalized"
	FromLink   Provenance = "link"
	Deduced    Provenance = "deduced"
)

type SourceRef struct {
	ID         string
	Accession  string
	Provenance Provenance
}

// Request is everything a check needs and nothing about which form of input
// produced it. Sheet is set only by a samplesheet: it carries the pairs the
// sheet declares, which the names alone do not say.
type Request struct {
	Files        []File
	Source       *SourceRef
	Sheet        *samplesheet.Sheet
	Skipped      []string
	SkippedLinks []string
}

func (r Request) Names() []secret.Name {
	names := make([]secret.Name, len(r.Files))
	for i, f := range r.Files {
		names[i] = f.Name
	}
	return names
}

// UsageError is a mistake in what the person typed. Nothing was read, and the
// CLI exits 1 with Message.
type UsageError struct {
	Message string
}

func (e *UsageError) Error() string { return e.Message }

func usage(format string, args ...any) *UsageError {
	return &UsageError{Message: fmt.Sprintf(format, args...)}
}

// Found is what one resolver contributes.
type Found struct {
	Files        []File
	Skipped      []string
	SkippedLinks []string
	Sheet        *samplesheet.Sheet
}

// Resolver turns one form of input into files.
type Resolver interface {
	Resolve(ctx context.Context) (Found, error)
}

// Inputs is what the person gave. Optional names are pointers, because a
// zero secret.Name is a valid (empty) name.
type Inputs struct {
	Paths       []secret.Name
	Recursive   bool
	ListFile    *secret.Name
	ListStdin   io.Reader
	Samplesheet *secret.Name
	Source      string
}

// Resolve checks that the forms given make sense together, runs each one and
// merges what they found. A source reference is resolved last, because
// deducing an accession needs the files.
func Resolve(ctx context.Context, in Inputs) (Request, error) {
	if err := in.validate(); err != nil {
		return Request{}, err
	}
	if !in.givesAnything() {
		return Request{}, usage("no path given; to check the files of the current folder, run: colligo check .")
	}
	var resolvers []Resolver
	switch {
	case in.Samplesheet != nil:
		resolvers = append(resolvers, SheetFile{Name: *in.Samplesheet})
	default:
		if len(in.Paths) > 0 {
			resolvers = append(resolvers, Paths{Names: in.Paths, Recursive: in.Recursive})
		}
		if in.ListFile != nil {
			resolvers = append(resolvers, FileList{Name: *in.ListFile})
		}
		if in.ListStdin != nil {
			resolvers = append(resolvers, ListFromStdin(in.ListStdin))
		}
	}
	req, err := Merge(ctx, resolvers...)
	if err != nil {
		return Request{}, err
	}
	if in.Source != "" {
		ref, err := ParseSource(in.Source, req.Files)
		if err != nil {
			return Request{}, err
		}
		req.Source = &ref
	}
	return req, nil
}

func (in Inputs) givesAnything() bool {
	return len(in.Paths) > 0 || in.ListFile != nil || in.ListStdin != nil || in.Samplesheet != nil
}

func (in Inputs) validate() error {
	switch {
	case in.Samplesheet != nil && len(in.Paths) > 0:
		return usage("--samplesheet lists the files; no other path is accepted")
	case in.Samplesheet != nil && (in.ListFile != nil || in.ListStdin != nil):
		return usage("--samplesheet and --files-from cannot be combined: each one says which files to read")
	case in.Samplesheet != nil && in.Source != "":
		return usage("--source and --samplesheet cannot be combined yet")
	case in.Samplesheet != nil && in.Recursive:
		return usage("--recursive does not apply: the samplesheet lists the files")
	case in.ListFile != nil && in.ListStdin != nil:
		return usage("--files-from was given twice")
	case in.Recursive && len(in.Paths) == 0 && (in.ListFile != nil || in.ListStdin != nil):
		return usage("--recursive does not apply to --files-from: the list names the files; give a folder as an argument to descend into it")
	}
	return nil
}

// Merge runs the resolvers and joins their files, in order. Each resolver
// decides about repetition inside its own input: a list or a samplesheet reads
// a path named twice once, while a path given twice as arguments is checked
// twice, as it always was.
func Merge(ctx context.Context, resolvers ...Resolver) (Request, error) {
	var req Request
	for _, r := range resolvers {
		found, err := r.Resolve(ctx)
		if err != nil {
			return Request{}, err
		}
		req.Files = append(req.Files, found.Files...)
		req.Skipped = append(req.Skipped, found.Skipped...)
		req.SkippedLinks = append(req.SkippedLinks, found.SkippedLinks...)
		if found.Sheet != nil {
			req.Sheet = found.Sheet
		}
	}
	return req, nil
}

func addFile(files []File, f File) []File {
	i := slices.IndexFunc(files, func(have File) bool { return fsaccess.SamePath(have.Name.Raw, f.Name.Raw) })
	if i < 0 {
		return append(files, f)
	}
	files[i].Origins = append(files[i].Origins, f.Origins...)
	return files
}

func describeLines(lines []int) string {
	slices.Sort(lines)
	parts := make([]string, len(lines))
	for i, l := range lines {
		parts[i] = fmt.Sprint(l)
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return "line " + parts[0]
	case 2:
		return "lines " + parts[0] + " and " + parts[1]
	}
	return "lines " + strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1]
}

// DescribeLines names lines the way a report does: "line 3", "lines 3 and 7",
// "lines 1, 2 and 5".
func DescribeLines(lines []int) string { return describeLines(slices.Clone(lines)) }

// NamesFromArguments turns the paths of the command line into names. A
// file:// address is read as the path it names.
func NamesFromArguments(args []string) ([]secret.Name, error) {
	names := make([]secret.Name, len(args))
	for i, a := range args {
		name, err := NameFromArgument(a)
		if err != nil {
			return nil, err
		}
		names[i] = name
	}
	return names, nil
}
