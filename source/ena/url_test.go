package ena

import (
	"net/url"
	"testing"
)

func TestReportURLAsksForEveryDeclaredField(t *testing.T) {
	parsed, err := url.Parse(New().reportURL("PRJEB1"))
	if err != nil {
		t.Fatal(err)
	}
	query := parsed.Query()
	want := map[string]string{
		"accession": "PRJEB1",
		"result":    "read_run",
		"format":    "tsv",
		"fields":    "run_accession,library_layout,fastq_ftp,fastq_md5,fastq_bytes,read_count,base_count,status",
	}
	for key, value := range want {
		if query.Get(key) != value {
			t.Errorf("%s = %q, want %q", key, query.Get(key), value)
		}
	}
	if parsed.Host != "www.ebi.ac.uk" || parsed.Path != "/ena/portal/api/filereport" {
		t.Errorf("host %s path %s", parsed.Host, parsed.Path)
	}
}
