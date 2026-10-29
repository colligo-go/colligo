// Package diagnoses is the versioned catalogue that turns a failed
// assertion into a sentence: what was observed, the probable cause and the
// suggested action. An assertion without an entry is a bug.
package diagnoses

import (
	"sort"
	"strings"

	"github.com/colligo-go/colligo/model"
)

const (
	ReadFailed         = "read_failed"
	HTMLBody           = "html_body"
	MagicMismatch      = "magic_mismatch"
	ChecksumMismatch   = "checksum_mismatch"
	SizeMismatch       = "size_mismatch"
	StructureInvalid   = "structure_invalid"
	FormatUnknown      = "format_unknown"
	FormatDeclared     = "format_declared"
	CountMismatch      = "count_mismatch"
	PairCount          = "pair_count"
	PairIDs            = "pair_ids"
	ExtensionMismatch  = "extension_mismatch"
	TrailingBlankLines = "trailing_blank_lines"
)

type Entry struct {
	ID          string
	Mode        string
	Severity    model.Severity
	Observation string
	Cause       string
	Action      string
}

type Values struct {
	Declared string
	Observed string
	Detail   string
}

func (e Entry) Describe(v Values) (observation, cause, action string) {
	r := strings.NewReplacer(
		"{declared}", v.Declared, "{observed}", v.Observed, "{detail}", v.Detail)
	return r.Replace(e.Observation), r.Replace(e.Cause), r.Replace(e.Action)
}

var catalogue = []Entry{
	{
		ID: TrailingBlankLines, Mode: "M4", Severity: model.Advisory,
		Observation: "The file ends with {observed} blank line(s) after the last complete record.",
		Cause:       "A tool that wrote or concatenated the file added empty lines; the records themselves are complete.",
		Action:      "No action is needed for the data; remove the blank lines if a downstream tool rejects them.",
	},
	{
		ID: ReadFailed, Mode: "M10", Severity: model.Fatal,
		Observation: "The file could not be read: {detail}.",
		Cause:       "The disk, the network filesystem or the permissions failed while reading.",
		Action:      "Check the path, the permissions and the health of the storage, then run the check again.",
	},
	{
		ID: HTMLBody, Mode: "M3", Severity: model.Fatal,
		Observation: "The file looks like a web page, not data: it starts with HTML.",
		Cause:       "The server answered with an error or login page and the download saved it under the data name.",
		Action:      "Delete the file and download it again; open it in an editor to read the server message if it keeps happening.",
	},
	{
		ID: MagicMismatch, Mode: "M3", Severity: model.Fatal,
		Observation: "The name says {declared}, but the first bytes say {observed}.",
		Cause:       "The file is not what its name or declaration claims, usually an error page or the wrong file saved under that name.",
		Action:      "Do not use the file. Download it again from the source and compare with the published checksum.",
	},
	{
		ID: ChecksumMismatch, Mode: "M4", Severity: model.Fatal,
		Observation: "The {detail} is {observed}, but {declared} was expected.",
		Cause:       "The bytes on disk differ from the bytes the source published: truncated or corrupted transfer, or a different file.",
		Action:      "Download the file again and compare the checksum before using it.",
	},
	{
		ID: SizeMismatch, Mode: "M4", Severity: model.Fatal,
		Observation: "The file has {observed} bytes, but {declared} bytes were expected.",
		Cause:       "The transfer stopped early or the file was replaced.",
		Action:      "Resume or repeat the download until the size matches.",
	},
	{
		ID: StructureInvalid, Mode: "M4", Severity: model.Blocking,
		Observation: "The {detail} structure is broken: {observed}.",
		Cause:       "The file ends before it should or contains damaged data, as a cut-off download or a failed write does.",
		Action:      "Download the file again; tools downstream may accept it silently and produce plausible but wrong numbers.",
	},
	{
		ID: FormatUnknown, Mode: "M4", Severity: model.Blocking,
		Observation: "The content is not a format Colligo recognises.",
		Cause:       "The file is not FASTQ, FASTA or GTF, or it is empty or damaged at the start.",
		Action:      "Declare the format with --format if you know it, or check where the file came from.",
	},
	{
		ID: FormatDeclared, Mode: "M4", Severity: model.Blocking,
		Observation: "The format was declared as {declared}, but the content is {observed}.",
		Cause:       "The declaration or the file name does not match what is inside the file.",
		Action:      "Correct the declaration or use the right file.",
	},
	{
		ID: CountMismatch, Mode: "M15", Severity: model.Blocking,
		Observation: "The file has {observed} {detail}, but {declared} were expected.",
		Cause:       "The content is valid but shorter or longer than the source declared, as with an extraction that shortened the reads.",
		Action:      "Extract or download the file again and compare the counts with the source record.",
	},
	{
		ID: PairCount, Mode: "M5", Severity: model.Blocking,
		Observation: "The mates have a different number of reads: {declared} against {observed}.",
		Cause:       "One of the two files lost reads, or they come from different samples paired by mistake.",
		Action:      "Do not align these files together. Download both again or fix the pairing.",
	},
	{
		ID: PairIDs, Mode: "M5", Severity: model.Blocking,
		Observation: "The mates do not start or end with the same read: {declared} against {observed}.",
		Cause:       "The files come from different samples or were sorted or filtered differently.",
		Action:      "Do not align these files together. Check which files belong to the same sample.",
	},
	{
		ID: ExtensionMismatch, Mode: "M4", Severity: model.Advisory,
		Observation: "The name suggests {declared}, but the content is {observed}.",
		Cause:       "The file was renamed or saved with a misleading extension.",
		Action:      "Rename the file so the name matches its content.",
	},
}

func Lookup(id string) (Entry, bool) {
	for _, e := range catalogue {
		if e.ID == id {
			return e, true
		}
	}
	return Entry{}, false
}

func IDs() []string {
	ids := make([]string, len(catalogue))
	for i, e := range catalogue {
		ids[i] = e.ID
	}
	sort.Strings(ids)
	return ids
}
