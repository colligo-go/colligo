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
	return []Reference{{
		Label:  "GRCh38 primary assembly, GENCODE human 50",
		Digest: "tmnbiAyj2fke68d_TYjq2g487US8C15r",
		Source: "computed by colligo from GRCh38.primary_assembly.genome.fa.gz, whose MD5 matched https://ftp.ebi.ac.uk/pub/databases/gencode/Gencode_human/release_50/MD5SUMS on 2026-11-01; the same digest, with the same names, lengths and sequences attributes, is served by https://seqcolapi.databio.org/collection/tmnbiAyj2fke68d_TYjq2g487US8C15r on 2026-11-01",
	}}
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
