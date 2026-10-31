package ena

import (
	"testing"
)

func TestReportQueryAsksForEveryDeclaredField(t *testing.T) {
	query := reportQuery("PRJEB1")
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
	if got := New().base.Join(query, "filereport").String(); got != "https://www.ebi.ac.uk/ena/portal/api/filereport" {
		t.Errorf("endpoint = %s", got)
	}
}
