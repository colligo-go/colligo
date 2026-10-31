package diagnoses

// Explanation is the long text behind a catalogue entry, written for a reader
// who is not a bioinformatician.
type Explanation struct {
	Means   string
	Why     string
	Do      string
	Confirm string
}

func Explain(id string) (Explanation, bool) {
	e, ok := explanations[id]
	return e, ok
}

var explanations = map[string]Explanation{
	TrailingBlankLines: {
		Means:   "The file is complete, but it ends with one or more empty lines after the last record.",
		Why:     "A script or a copy-and-paste step added newlines at the end. Some tools write a final empty line, and joining files can leave several.",
		Do:      "Nothing is needed for the data. If a program downstream refuses the file, remove the empty lines at the end.",
		Confirm: "Look at the end of the file: 'tail -c 50 file | od -c' for plain text, or 'zcat file.gz | tail -c 50 | od -c' for a .gz file. Newline characters with nothing between them are the empty lines.",
	},
	ReadFailed: {
		Means:   "Colligo could not read the file, so it checked nothing about its content.",
		Why:     "The file could not be opened or read to the end: wrong permissions, a broken symbolic link, an error on the disk or the network drive, or a file removed while the check started.",
		Do:      "Fix the cause (permissions, the path, the drive) and run the check again. Until then, treat the file as unchecked.",
		Confirm: "Try 'head -c 1 file'. If that fails too, the problem is outside Colligo, and the error text in the report says what the system answered.",
	},
	ChangedDuringRead: {
		Means:   "The file changed while Colligo was reading it, so the checksums and counts it computed may describe bytes that were never on the disk together. They are not trusted.",
		Why:     "Another program was still writing to the file, for example a download or a copy in progress, or the file was truncated, renamed or replaced during the check.",
		Do:      "Wait until nothing is writing to the file, then run the check again.",
		Confirm: "Run 'ls -l file' twice a few seconds apart: a size or time that changes means something is still writing it. 'lsof file' lists the programs that have it open.",
	},
	HTMLBody: {
		Means:   "The file is not data: it starts like a web page.",
		Why:     "A download failed and the server answered with an error, login or 'too many requests' page. The downloader saved that page under the name of the data file.",
		Do:      "Delete the file and download it again. If it happens again, open the file in a text editor: the page usually says why, such as a login that is needed, a rate limit or a wrong address.",
		Confirm: "'head -c 300 file' shows text that starts with <html or <!DOCTYPE. This works even for a name ending in .gz, because the page was never compressed.",
	},
	MagicMismatch: {
		Means:   "The first bytes of the file do not match what its name or its declared format says, such as a .gz file that is not gzip.",
		Why:     "The file was renamed, the wrong file was saved under this name, or an error page replaced the data.",
		Do:      "Do not use the file. Download it again from the source and compare it with the published checksum.",
		Confirm: "'file name' prints what the file really is. 'head -c 16 file | od -c' shows the first bytes: a gzip file starts with the two bytes 1f 8b.",
	},
	ChecksumMismatch: {
		Means:   "The checksum Colligo computed is not the one that was expected, so these bytes are not the bytes the source published.",
		Why:     "The transfer was cut or corrupted, the file was changed after the download (uncompressed, edited or compressed again), or it is a different file or version.",
		Do:      "Download the file again and compare the checksum before using it. If you changed the file on purpose, compare with the checksum of your changed version instead.",
		Confirm: "Compute it yourself with 'md5sum file' (or 'sha256sum file') and compare it with the published value.",
	},
	SizeMismatch: {
		Means:   "The size of the file is not the expected size.",
		Why:     "The transfer stopped early, or the file was replaced, extracted or truncated.",
		Do:      "Resume or repeat the download until the size matches.",
		Confirm: "'stat -c %s file' prints the size in bytes. Compare it with the expected size.",
	},
	StructureInvalid: {
		Means:   "The file is the right kind of file, but its inside is broken: it ends too early or holds damaged records.",
		Why:     "Usually an interrupted download or copy, a disk that filled up while writing, or a compression error. Many tools accept such a file silently and give results that look fine but rest on part of the data.",
		Do:      "Download or copy the file again. Do not analyse the damaged one.",
		Confirm: "For a gzip file, 'gzip -t file.gz' prints an error such as 'unexpected end of file' when it is cut. For FASTQ, the number of lines must be a multiple of 4: 'zcat file.gz | wc -l'.",
	},
	FormatUnknown: {
		Means:   "Colligo could not tell what kind of data the file holds.",
		Why:     "The file is not FASTQ, FASTA or GTF, which are the formats this version understands, or it is empty, or it is damaged at its start.",
		Do:      "If you know the format, say it with --format (fastq, fasta, gtf, gzip or bgzf). Otherwise check where the file came from.",
		Confirm: "Read the first lines ('head file', or 'zcat file.gz | head'): FASTQ records start with @ and FASTA records start with >.",
	},
	FormatDeclared: {
		Means:   "A flag, a name or a samplesheet says the file is one format, but its content is another.",
		Why:     "The declaration is wrong, or the file is the wrong one.",
		Do:      "Correct the declaration or use the right file.",
		Confirm: "Read the first lines of the file: FASTQ records start with @, FASTA with >, and a GTF has nine columns separated by tabs.",
	},
	CountMismatch: {
		Means:   "The file is well formed, but it holds a different amount of data from what was declared: fewer or more reads or bases.",
		Why:     "A tool may have shortened or trimmed the reads, the file may have been extracted or converted in a way that lost data, or you may hold another version of the data than the one the source describes. Nothing looks broken, which is why only a count catches it.",
		Do:      "Extract or download the file again and compare the counts with the source record. Do not use the data until the difference is explained.",
		Confirm: "Count the reads with 'zcat file.fastq.gz | wc -l' and divide by 4. The source record lists its own numbers: for ENA, the base_count and read_count fields of the run.",
	},
	PairCount: {
		Means:   "The two mate files of a pair (R1 and R2) hold different numbers of reads.",
		Why:     "One file lost reads, for example in a cut transfer or a filter applied to one file only, or the two files do not belong together.",
		Do:      "Do not align these files together. Download both again or fix the pairing.",
		Confirm: "Run 'zcat R1.fastq.gz | wc -l' and the same for R2. For a proper pair the two numbers are equal.",
	},
	PairIDs: {
		Means:   "The reads of a pair must appear in the same order in both mate files, but the first or the last read names differ.",
		Why:     "The files come from different samples, or one of them was sorted or filtered on its own.",
		Do:      "Do not align these files together. Check which files belong to the same sample.",
		Confirm: "Compare the first read name of each file: 'zcat R1.fastq.gz | head -1' and 'zcat R2.fastq.gz | head -1'. They are the same except for an ending such as /1 and /2.",
	},
	PairMissing: {
		Means:   "The name says this file is one mate of a pair (_1, _R1 and so on), but its partner was not among the files checked.",
		Why:     "The partner is in another folder or was left out of the command, or the data is single-end and the name only looks like a mate.",
		Do:      "Add the other mate to the check so Colligo can confirm the pair. If the data is single-end, ignore this note.",
		Confirm: "List the folder with 'ls' and look for the same name with _2 or _R2.",
	},
	ExtensionMismatch: {
		Means:   "The name of the file suggests one format, but its content is another.",
		Why:     "The file was renamed or saved with a misleading extension.",
		Do:      "Rename the file so the name matches its content. The data itself is not affected.",
		Confirm: "Run 'file name' to see what the content is.",
	},
	SheetRemotePath: {
		Means:   "The samplesheet lists a web or cloud address instead of a local file. Colligo checks local files, so nothing about that entry was checked.",
		Why:     "Some pipelines accept remote data. Colligo does not download anything on its own, so it cannot verify it.",
		Do:      "Download the file, point the samplesheet at the local copy and check again.",
		Confirm: "The report gives the line of the samplesheet. Open the CSV at that line to see the address.",
	},
	SheetSharedFile: {
		Means:   "Two entries of the samplesheet point at files with exactly the same content.",
		Why:     "The same data was listed twice under different names or samples, so a pipeline would count it twice or give two samples identical reads.",
		Do:      "Fix the line that points to the wrong file, or remove the repeated entry.",
		Confirm: "Run 'md5sum' on the files named in the report. Identical checksums mean identical content.",
	},
	SheetDuplicateRow: {
		Means:   "A line of the samplesheet repeats a sample with the same files as another line.",
		Why:     "The line was copied. Lines with the same sample name are meant for different lanes, each with its own files. With the same files, the pipeline merges the data with itself.",
		Do:      "Remove the repeated line, or give it the files of the lane it stands for.",
		Confirm: "Open the samplesheet at the lines named in the report: the sample name and the file columns are identical.",
	},
	SheetMatesSwapped: {
		Means:   "The second-mate files of two samplesheet lines look exchanged: each one fits the first-mate file of the other line.",
		Why:     "The columns of the sheet were edited or sorted separately, so the mates ended up with the wrong sample.",
		Do:      "Exchange the fastq_2 values of the two lines named in the report.",
		Confirm: "Compare the first read names of the fastq_1 and fastq_2 files of each line with 'zcat file | head -1'. The names of a pair match.",
	},
	SourceStatus: {
		Means:   "The archive says the run is not public: it is suppressed, withdrawn or restricted. The files on your disk are unchanged, but the archive no longer stands behind them.",
		Why:     "Archives suppress runs when the data is found to be wrong or is withdrawn by whoever submitted it. Nothing on your disk changes when that happens, so only a comparison with the source shows it.",
		Do:      "Read the archive record of the run to learn why, and do not use the data until you know.",
		Confirm: "Open the run on the archive website and read its status. For ENA the address is https://www.ebi.ac.uk/ena/browser/view/ followed by the accession.",
	},
	SourceFileMissing: {
		Means:   "The source lists a file for this run that is not among the files you checked.",
		Why:     "The file was not downloaded, it is in another folder that you did not name, or it was renamed.",
		Do:      "Download the missing file or add it to the command. The run is incomplete without it.",
		Confirm: "List the folder and look for the name in the report. The source page of the run has its file list.",
	},
	SourceFileUnlisted: {
		Means:   "A file you checked is not one that the source lists for the run.",
		Why:     "It belongs to another run, it was renamed after the download, or it is a second copy of a listed file.",
		Do:      "Check that the file belongs here. If it is a renamed download, restore the name the source uses.",
		Confirm: "Compare the name of the file with the list of files on the source page of the run.",
	},
	SourceFileAmbiguous: {
		Means:   "More than one file you checked stands for the same file of the run, so Colligo could not tell which one to compare with the source and compared none of them.",
		Why:     "The same file name, or the same content, appears in more than one of the folders checked, for example two downloads of the same run.",
		Do:      "Check only one copy: name its folder, or the files themselves.",
		Confirm: "'find . -name <file name>' lists every copy.",
	},
	ReadCountUnexplained: {
		Means:   "The number of reads the source declares for the run matches none of the ways Colligo knows it can be counted. The bases agree, so the content is intact.",
		Why:     "The read_count field does not mean the same thing for every run: it can be the reads of each file, of the pairs, or something else. This run uses a meaning Colligo has not seen yet.",
		Do:      "Nothing is needed for the data. Report the accession so that the reading can be added.",
		Confirm: "Count the reads of each file ('zcat file | wc -l', divided by 4) and compare with the declared read_count: per file, as a sum, or counted per pair.",
	},
	LayoutMismatch: {
		Means:   "The archive labels the run as single-end, but its files form a pair. The data is fine; the label is not.",
		Why:     "The library layout recorded at the source is wrong.",
		Do:      "Treat the files as the mates of one sample. A pipeline that trusts the label would treat them as two separate single-end samples.",
		Confirm: "Compare the read names of the two files with 'zcat file | head -4'. Matching names ending in /1 and /2 mean a pair.",
	},
	SourceRowRejected: {
		Means:   "The record of the run at the source was malformed, so Colligo could not use what it declares about that run.",
		Why:     "The archive returned a row with an unreadable or inconsistent field.",
		Do:      "Query the source again later. If the row stays malformed, report it to the archive.",
		Confirm: "Open the run on the archive website and see whether the same field looks wrong there.",
	},
	SourceRunFailed: {
		Means:   "This file belongs to a run that failed against the source. The file may be fine on its own, but none of the files of the run can be trusted alone.",
		Why:     "A run is judged as a whole. The failure is in the totals or the status of the run, not in one file.",
		Do:      "Read the report of the run, fix what it names and check again.",
		Confirm: "The issue of the run in the same report says what failed. Run 'colligo explain' with its id for more.",
	},
	SourceFieldEmpty: {
		Means:   "The archive left a field empty for this run, so the comparison that needs it was skipped, not failed.",
		Why:     "Not every archive record is filled in. An empty value means the source declares nothing, which is not the same as declaring zero.",
		Do:      "Nothing is needed for the data. The Not checked list of the report names the comparison that could not be made.",
		Confirm: "Open the run on the archive website and look for the empty field.",
	},
}
