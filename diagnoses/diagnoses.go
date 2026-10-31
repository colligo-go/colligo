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
	ChangedDuringRead  = "changed_during_read"
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
	PairMissing        = "pair_missing"
	ExtensionMismatch  = "extension_mismatch"
	TrailingBlankLines = "trailing_blank_lines"
	SheetRemotePath    = "sheet_remote_path"
	SheetSharedFile    = "sheet_shared_file"
	SheetDuplicateRow  = "sheet_duplicate_row"
	SheetMatesSwapped  = "sheet_mates_swapped"

	SourceStatus         = "source_status"
	SourceFileMissing    = "source_file_missing"
	SourceFileUnlisted   = "source_file_unlisted"
	SourceFileAmbiguous  = "source_file_ambiguous"
	ReadCountUnexplained = "read_count_unexplained"
	LayoutMismatch       = "layout_mismatch"
	SourceRowRejected    = "source_row_rejected"
	SourceFieldEmpty     = "source_field_empty"
	SourceRunFailed      = "source_run_failed"
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
		ID: ChangedDuringRead, Mode: "M2", Severity: model.Blocking,
		Observation: "The file changed while it was being read; run the check again.",
		Cause:       "Another process wrote to, truncated or replaced the file during the read ({observed}), so the hashes and facts may describe bytes that never existed together.",
		Action:      "Wait until nothing is writing to the file, then run the check again.",
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
		Observation: "The content has {observed} {detail}, but {declared} were expected.",
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
		ID: PairMissing, Mode: "M5", Severity: model.Advisory,
		Observation: "The name says this file is one mate of a pair, but its other mate {declared} is not among the files checked.",
		Cause:       "The other mate was left out of the command or is in another directory, or the data is single-end and the name carries a mate suffix.",
		Action:      "Add the other mate to the same run so the pair can be confirmed; if the data is single-end, no action is needed.",
	},
	{
		ID: ExtensionMismatch, Mode: "M4", Severity: model.Advisory,
		Observation: "The name suggests {declared}, but the content is {observed}.",
		Cause:       "The file was renamed or saved with a misleading extension.",
		Action:      "Rename the file so the name matches its content.",
	},
	{
		ID: SheetRemotePath, Mode: "M6", Severity: model.Blocking,
		Observation: "Samplesheet line {detail} points to a remote location ({observed}), which this version does not check.",
		Cause:       "The samplesheet refers to data by URL, and Colligo checks only local files here; nothing was downloaded and nothing about that entry was verified.",
		Action:      "Download the file and point the samplesheet at the local copy, then check again.",
	},
	{
		ID: SheetSharedFile, Mode: "M19", Severity: model.Blocking,
		Observation: "Samplesheet {detail}: the files listed have identical content (BLAKE3 {declared}).",
		Cause:       "The same data was listed twice under different names or samples, so a pipeline would count it twice or give two samples the same reads.",
		Action:      "Fix the line that points at the wrong file, or remove the repeated entry.",
	},
	{
		ID: SheetDuplicateRow, Mode: "M19", Severity: model.Blocking,
		Observation: "Samplesheet lines {declared} repeat sample {detail} with the same files.",
		Cause:       "A row was copied. Repeated lines of one sample are lanes only when each has different files; with the same files the pipeline merges the reads with themselves.",
		Action:      "Remove the repeated line, or give it the files of the lane it stands for.",
	},
	{
		ID: SheetMatesSwapped, Mode: "M5", Severity: model.Blocking,
		Observation: "The fastq_2 files of samplesheet lines {declared} and {observed} look swapped: each one fits the fastq_1 of the other line.",
		Cause:       "The columns were edited or sorted independently, so the mates of two samples were exchanged.",
		Action:      "Exchange the fastq_2 values of those two lines.",
	},
	{
		ID: SourceStatus, Mode: "M16", Severity: model.Blocking,
		Observation: "The source marks run {detail} as {observed}, not {declared}.",
		Cause:       "The archive suppressed, withdrew or restricted the run, usually because the data was found to be wrong; nothing on the disk changes when that happens.",
		Action:      "Read the archive record of the run to learn why, and do not use the data until you know.",
	},
	{
		ID: SourceFileMissing, Mode: "M6", Severity: model.Blocking,
		Observation: "Run {detail} lists {declared}, but no file checked stands for it.",
		Cause:       "The file was not downloaded, was left out of the command, or was renamed.",
		Action:      "Download the missing file or add it to the command; the run is incomplete without it.",
	},
	{
		ID: SourceFileUnlisted, Mode: "M1", Severity: model.Advisory,
		Observation: "The file {observed} is not one the source lists for {declared}.",
		Cause:       "The file belongs to another run, was renamed after download, or is a second copy of a listed file.",
		Action:      "Check that the file belongs here; if it is a renamed download, restore the name the source uses.",
	},
	{
		ID: SourceFileAmbiguous, Mode: "M1", Severity: model.Blocking,
		Observation: "{observed} files stand for {declared} of run {detail}, so none of them was compared with the source.",
		Cause:       "The same file name appears in more than one directory checked, and Colligo cannot tell which copy the run should be judged on.",
		Action:      "Check only one copy, by naming its directory or the files themselves.",
	},
	{
		ID: ReadCountUnexplained, Mode: "M15", Severity: model.Advisory,
		Observation: "Run {detail} declares {declared} reads, which matches no known reading of the files ({observed}).",
		Cause:       "The bases agree with the source, so the content is intact; the read_count field of the record means something not yet seen.",
		Action:      "No action is needed for the data. Report the accession so the reading can be added as a fixture.",
	},
	{
		ID: LayoutMismatch, Mode: "M15", Severity: model.Advisory,
		Observation: "Run {detail} is labelled {declared}, but its files form a pair ({observed}).",
		Cause:       "The library layout recorded at the source is wrong; the data itself is intact.",
		Action:      "Treat the files as mates of one sample; a pipeline that trusts the label would treat them as two single-end samples.",
	},
	{
		ID: SourceRowRejected, Mode: "M15", Severity: model.Blocking,
		Observation: "The source record for {detail} could not be used: {observed}.",
		Cause:       "The source returned a malformed row, so what it declares about that run could not be compared with the files.",
		Action:      "Query the source again later; if the row stays malformed, report it to the archive.",
	},
	{
		ID: SourceRunFailed, Mode: "M15", Severity: model.Blocking,
		Observation: "The file belongs to run {detail}, which failed {observed} against the source.",
		Cause:       "The file may be valid on its own, but the run it is part of does not match what the source declares, so none of its files can be trusted alone.",
		Action:      "Read the report of the run, fix what it names, and check again.",
	},
	{
		ID: SourceFieldEmpty, Mode: "M15", Severity: model.Advisory,
		Observation: "The source record for {detail} is incomplete: {observed}.",
		Cause:       "The archive left a field of the run empty; the comparison that needs it is skipped, not failed.",
		Action:      "No action is needed for the data; the report says which comparison was not made.",
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
