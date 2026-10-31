package verifier

import (
	"crypto/md5"
	"encoding/hex"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/identity"
	"github.com/colligo-go/colligo/secret"
)

const gencodeHeader = "##description: evidence-based annotation of the human genome (GRCh38), version 44 (Ensembl 110)\n" +
	"##provider: GENCODE\n##contact: gencode-help@ebi.ac.uk\n##format: gtf\n##date: 2023-03-01\n"

func gencodeFeatures(chrom, typeKey string) string {
	attrs := func(extra string) string {
		return `gene_id "ENSG00000290825.1"; ` + extra + typeKey + ` "lncRNA"; gene_name "DDX11L2"; level 2;`
	}
	var b strings.Builder
	b.WriteString(chrom + "\tHAVANA\tgene\t11869\t14409\t.\t+\t.\t" + attrs("") + "\n")
	b.WriteString(chrom + "\tHAVANA\ttranscript\t11869\t14409\t.\t+\t.\t" + attrs(`transcript_id "ENST00000456328.2"; `) + "\n")
	b.WriteString(chrom + "\tHAVANA\texon\t11869\t12227\t.\t+\t.\t" + attrs(`transcript_id "ENST00000456328.2"; `) + ` exon_number 1;` + "\n")
	b.WriteString(chrom + "\tENSEMBL\tgene\t14404\t29570\t.\t-\t.\t" + attrs("") + "\n")
	return b.String()
}

func gencodeGTF() []byte { return []byte(gencodeHeader + gencodeFeatures("chr1", "gene_type")) }

func md5Of(data []byte) string {
	sum := md5.Sum(data)
	return hex.EncodeToString(sum[:])
}

func checksumTable(label string, data []byte) identity.Table {
	return identity.Table{Checksums: []identity.Checksum{{
		Label: label, Asset: "gencode.v44.annotation.gtf.gz", MD5: md5Of(data),
		Origin: "https://example.org/MD5SUMS", Retrieved: "2026-10-31",
	}}}
}

func identifyWith(t *testing.T, table identity.Table, name secret.Name) *Identification {
	t.Helper()
	got, err := IdentifyFile(t.Context(), name, table)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func exitOf(got *Identification) int {
	return IdentifyReport{Files: []*Identification{got}}.ExitCode()
}

func TestGencodeHeaderIsDeclaredWithReleaseOrganismAndAssembly(t *testing.T) {
	dir := t.TempDir()
	for base, data := range map[string][]byte{
		"annotation.gtf":    gencodeGTF(),
		"annotation.gtf.gz": gzipped(t, gencodeGTF()),
	} {
		got := identifyWith(t, identity.Table{}, write(t, dir, base, data))
		if got.Status != IdentityDeclared || got.Gencode == nil {
			t.Fatalf("%s: %+v", base, got)
		}
		want := identity.Gencode{Label: "GENCODE 44", Release: "44", Organism: "human", Assembly: "GRCh38", Ensembl: "110", Date: "2023-03-01"}
		if *got.Gencode != want {
			t.Errorf("%s: gencode = %+v, want %+v", base, *got.Gencode, want)
		}
		if got.Checksum != nil || got.Reference != nil || got.Reason == "" {
			t.Errorf("%s: a declaration must not read as an identification: %+v", base, got)
		}
		if exitOf(got) != ExitOK {
			t.Errorf("%s: exit %d", base, exitOf(got))
		}
	}
}

func TestGencodeGtfWhoseChecksumIsInTheTableIsIdentified(t *testing.T) {
	dir := t.TempDir()
	plain, zipped := gencodeGTF(), gzipped(t, gencodeGTF())
	for base, data := range map[string][]byte{"annotation.gtf": plain, "annotation.gtf.gz": zipped} {
		table := checksumTable("GENCODE human 44", data)
		got := identifyWith(t, table, write(t, dir, base, data))
		if got.Status != IdentityIdentified || got.Checksum == nil || got.Checksum.Label != "GENCODE human 44" || got.Checksum.MD5 != md5Of(data) {
			t.Fatalf("%s: %+v", base, got)
		}
		if got.Gencode == nil || got.Gencode.Release != "44" || got.Reason != "" {
			t.Errorf("%s: the header facts are lost or a reason is left behind: %+v", base, got)
		}
	}
	other := identifyWith(t, checksumTable("GENCODE human 44", plain), write(t, dir, "other.gtf", append(plain[:len(plain):len(plain)], '#', '\n')))
	if other.Status != IdentityDeclared || other.Checksum != nil {
		t.Errorf("one more line must not match the checksum: %+v", other)
	}
}

func TestGencodeHeaderOverEnsemblContentIsAConflictAndExits3(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]struct {
		data []byte
		want string
	}{
		"ensembl-names.gtf":       {[]byte(gencodeHeader + gencodeFeatures("1", "gene_type") + gencodeFeatures("MT", "gene_type")), "Ensembl convention"},
		"ensembl-names-mixed.gtf": {[]byte(gencodeHeader + gencodeFeatures("chr1", "gene_type") + gencodeFeatures("X", "gene_type")), "mix the chr convention"},
		"gene-biotype.gtf":        {[]byte(gencodeHeader + gencodeFeatures("chr1", "gene_biotype")), "gene_biotype"},
		"both.gtf.gz":             {gzipped(t, []byte(gencodeHeader+gencodeFeatures("MT", "gene_biotype"))), "gene_biotype"},
	}
	for base, c := range cases {
		got := identifyWith(t, identity.Table{}, write(t, dir, base, c.data))
		if got.Status != IdentityConflict || !strings.Contains(got.Reason, c.want) || !strings.Contains(got.Reason, "GENCODE 44") {
			t.Errorf("%s: %s %q", base, got.Status, got.Reason)
		}
		if got.Gencode == nil || exitOf(got) != ExitIncomplete {
			t.Errorf("%s: gencode=%v exit=%d", base, got.Gencode, exitOf(got))
		}
	}
}

func TestGtfWithoutAGencodeHeaderIsNotIdentifiedAndExits0(t *testing.T) {
	dir := t.TempDir()
	cases := map[string]string{
		"no-header.gtf":     gencodeFeatures("chr1", "gene_type"),
		"ensembl.gtf":       "#!genome-build GRCh38.p14\n#!genome-version GRCh38\n" + gencodeFeatures("1", "gene_biotype"),
		"provider-only.gtf": "##provider: GENCODE\n" + gencodeFeatures("chr1", "gene_type"),
		"late-header.gtf":   gencodeFeatures("chr1", "gene_type") + gencodeHeader,
	}
	for base, text := range cases {
		got := identifyWith(t, identity.Table{}, write(t, dir, base, []byte(text)))
		if got.Status != IdentityNotIdentified || got.Reason == "" || got.Gencode != nil || exitOf(got) != ExitOK {
			t.Errorf("%s: %+v exit=%d", base, got, exitOf(got))
		}
	}
	if got := identifyWith(t, identity.Table{}, write(t, dir, "p.gtf", []byte(cases["provider-only.gtf"]))); !strings.Contains(got.Reason, "no release") {
		t.Errorf("provider-only reason: %s", got.Reason)
	}
}

func TestTruncatedGtfIsInvalidAndExits3EvenWithAGencodeHeader(t *testing.T) {
	dir := t.TempDir()
	full := gencodeGTF()
	cases := map[string][]byte{
		"cut-mid-line.gtf":  full[:len(full)-20],
		"cut-gz.gtf.gz":     gzipped(t, full)[:len(gzipped(t, full))-8],
		"header-only.gtf":   []byte(gencodeHeader),
		"wrong-columns.gtf": []byte(gencodeHeader + "chr1\tHAVANA\tgene\n"),
	}
	for base, data := range cases {
		got := identifyWith(t, checksumTable("x", data), write(t, dir, base, data))
		if got.Status != StatusInvalid || got.Gencode != nil || got.Checksum != nil || exitOf(got) != ExitIncomplete {
			t.Errorf("%s: %+v exit=%d", base, got, exitOf(got))
		}
	}
}

func TestFastaWhoseChecksumIsInTheTableIsIdentifiedByTheChecksum(t *testing.T) {
	dir := t.TempDir()
	data := []byte(identifyText)
	got := identifyWith(t, checksumTable("tiny genome", data), write(t, dir, "genome.fa", data))
	if got.Status != IdentityIdentified || got.Checksum == nil || got.Checksum.Label != "tiny genome" {
		t.Fatalf("got %+v", got)
	}
	if got.Reference != nil || got.Seqcol == nil || got.Seqcol.Digest != identifyDigest {
		t.Errorf("the seqcol digest is still reported and no collection claims the match: %+v", got)
	}
	changed := identifyWith(t, checksumTable("tiny genome", data), write(t, dir, "changed.fa", []byte(strings.Replace(identifyText, "GGGGCCCC", "GGGGCCCA", 1))))
	if changed.Status != IdentityNotIdentified || changed.Checksum != nil {
		t.Errorf("a changed base must not match: %+v", changed)
	}
	zipped := gzipped(t, data)
	gz := identifyWith(t, checksumTable("tiny genome", zipped), write(t, dir, "genome.fa.gz", zipped))
	if gz.Status != IdentityIdentified || gz.Checksum == nil {
		t.Errorf("the checksum of a compressed file is the one of its bytes: %+v", gz)
	}
}

func TestChecksumDoesNotUseTheNameOfTheFile(t *testing.T) {
	dir := t.TempDir()
	data := []byte(identifyText)
	got := identifyWith(t, checksumTable("tiny genome", data), write(t, dir, "gencode.v44.annotation.gtf.gz", data))
	if got.Status != IdentityIdentified {
		t.Errorf("the checksum alone decides, not the name: %+v", got)
	}
}

func TestIdentifyTextNamesTheEvidence(t *testing.T) {
	dir := t.TempDir()
	plain := gencodeGTF()
	report, err := IdentifyAll(t.Context(), []secret.Name{
		write(t, dir, "declared.gtf", plain),
		write(t, dir, "conflict.gtf", []byte(gencodeHeader+gencodeFeatures("1", "gene_type"))),
	}, identity.Table{})
	if err != nil {
		t.Fatal(err)
	}
	var out strings.Builder
	report.WriteText(&out)
	for _, want := range []string{"declared: GENCODE 44", "release: 44", "organism: human", "assembly: GRCh38", "conflict: the header says GENCODE 44"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, out.String())
		}
	}
	if report.ExitCode() != ExitIncomplete {
		t.Errorf("exit %d", report.ExitCode())
	}
	identified, err := IdentifyAll(t.Context(), []secret.Name{write(t, dir, "id.gtf", plain)}, checksumTable("GENCODE human 44", plain))
	if err != nil {
		t.Fatal(err)
	}
	out.Reset()
	identified.WriteText(&out)
	for _, want := range []string{"identified: GENCODE human 44", "md5: " + md5Of(plain), "https://example.org/MD5SUMS, read on 2026-10-31"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("text lacks %q:\n%s", want, out.String())
		}
	}
}

func TestGiantHeaderDoesNotBreakTheRead(t *testing.T) {
	dir := t.TempDir()
	filler := strings.Repeat("# "+strings.Repeat("x", 78)+"\n", 40_000)
	longLine := "#" + strings.Repeat("y", 3<<20) + "\n"
	features := gencodeFeatures("chr1", "gene_type")
	cases := map[string]struct {
		text   string
		status string
	}{
		"description-first.gtf": {gencodeHeader + filler + features, IdentityDeclared},
		"description-last.gtf":  {filler + gencodeHeader + features, IdentityNotIdentified},
		"one-huge-line.gtf":     {longLine + gencodeHeader + features, IdentityNotIdentified},
		"huge-line-after.gtf":   {gencodeHeader + longLine + features, IdentityDeclared},
	}
	for base, c := range cases {
		got := identifyWith(t, identity.Table{}, write(t, dir, base, []byte(c.text)))
		if got.Status != c.status || got.Size != int64(len(c.text)) {
			t.Errorf("%s: status %s (want %s), size %d (want %d), reason %q", base, got.Status, c.status, got.Size, len(c.text), got.Reason)
		}
		if c.status == IdentityNotIdentified && !strings.Contains(got.Reason, "longer than") {
			t.Errorf("%s: the reason does not say the header was cut: %q", base, got.Reason)
		}
	}
}
