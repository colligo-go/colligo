package format

import (
	"bytes"
	"encoding/json"
	"runtime"
	"testing"

	"github.com/colligo-go/colligo/identity"
)

func headerOf(t *testing.T, v FormatValidator, feed func(Extractor)) identity.GTFHeader {
	t.Helper()
	e := ExtractorFor(v, true)
	feed(e)
	facts, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range facts {
		if f.Kind == identity.GTFHeaderKind {
			var h identity.GTFHeader
			if err := json.Unmarshal(f.Value, &h); err != nil {
				t.Fatal(err)
			}
			return h
		}
	}
	t.Fatalf("no %s fact among %d facts", identity.GTFHeaderKind, len(facts))
	return identity.GTFHeader{}
}

const (
	testDescription = "##description: evidence-based annotation of the human genome (GRCh38), version 44 (Ensembl 110)\n"
	testFeature     = "chr1\tHAVANA\tgene\t11869\t14409\t.\t+\t.\tgene_id \"ENSG00000290825.1\"; gene_type \"lncRNA\";\n"
)

func TestGtfHeaderFactCarriesTheDeclaredFields(t *testing.T) {
	v, _ := Lookup(ContentGTF)
	h := headerOf(t, v, func(e Extractor) {
		e.Write([]byte(testDescription + "##provider: GENCODE\n##format: gtf\n##date: 2023-03-01\n##description: a second one is ignored\n" + testFeature))
	})
	if h.Provider != "GENCODE" || h.Format != "gtf" || h.Date != "2023-03-01" || h.HeaderLines != 5 || h.Truncated || h.GeneBiotypeLines != 0 {
		t.Errorf("header = %+v", h)
	}
	if got, ok := identity.ParseGencode(h); !ok || got.Release != "44" {
		t.Errorf("the first description line must win: %+v, %v", got, ok)
	}
}

func TestGtfHeaderFactDoesNotExistWithoutIdentify(t *testing.T) {
	v, _ := Lookup(ContentGTF)
	e := v.NewExtractor()
	e.Write([]byte(testDescription + testFeature))
	facts, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range facts {
		if f.Kind == identity.GTFHeaderKind {
			t.Error("check must not pay for the header fact")
		}
	}
}

func TestGeneBiotypeIsCountedOnlyAsAWholeAttributeKey(t *testing.T) {
	v, _ := Lookup(ContentGTF)
	lines := "chr1\ts\tgene\t1\t2\t.\t+\t.\tgene_id \"a\"; gene_biotype \"x\";\n" +
		"chr1\ts\tgene\t1\t2\t.\t+\t.\tgene_biotype=\"x\"\n" +
		"chr1\ts\tgene\t1\t2\t.\t+\t.\tnot_gene_biotype \"x\"; gene_biotype_list \"y\";\n" +
		"chr1\ts\tgene\t1\t2\t.\t+\t.\tnote \"gene_biotype\"; gene_type \"z\";\n" +
		"chr1\ts\texon\t1\t2\t.\t+\t.\tgene_biotype \"x\";\n" +
		"chr1\ts\tgene_segment\t1\t2\t.\t+\t.\tgene_biotype \"x\";\n"
	if h := headerOf(t, v, func(e Extractor) { e.Write([]byte(lines)) }); h.GeneBiotypeLines != 2 {
		t.Errorf("gene_biotype lines = %d, want 2: only whole keys on gene lines", h.GeneBiotypeLines)
	}
}

func TestGiantHeaderCostsBoundedMemoryWhateverItsSize(t *testing.T) {
	v, _ := Lookup(ContentGTF)
	chunk := bytes.Repeat([]byte("# "+string(bytes.Repeat([]byte("x"), 62))+"\n"), 16384)
	oneLine := bytes.Repeat([]byte("z"), 1<<20)
	cases := map[string]func(Extractor){
		"many comment lines": func(e Extractor) {
			e.Write([]byte(testDescription))
			for range 256 {
				e.Write(chunk)
			}
			e.Write([]byte(testFeature))
		},
		"one endless line": func(e Extractor) {
			e.Write([]byte("#"))
			for range 256 {
				e.Write(oneLine)
			}
			e.Write([]byte("\n" + testDescription + testFeature))
		},
	}
	for name, feed := range cases {
		var before, after runtime.MemStats
		runtime.GC()
		runtime.ReadMemStats(&before)
		h := headerOf(t, v, feed)
		runtime.ReadMemStats(&after)
		if grown := after.TotalAlloc - before.TotalAlloc; grown > 4<<20 {
			t.Errorf("%s: %d MiB allocated while reading a 256 MiB header", name, grown>>20)
		}
		if !h.Truncated && name == "one endless line" {
			t.Errorf("%s: an endless line must flag the header as truncated: %+v", name, h)
		}
	}
}

func TestGtfFeaturesAfterAGiantHeaderAreStillCounted(t *testing.T) {
	v, _ := Lookup(ContentGTF)
	e := ExtractorFor(v, true)
	chunk := bytes.Repeat([]byte("#\n"), 1<<20)
	e.Write([]byte(testDescription))
	for range 8 {
		e.Write(chunk)
	}
	e.Write([]byte(testFeature + testFeature))
	facts, err := e.Finish()
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range facts {
		if f.Kind == "records" {
			var r struct{ Records int64 }
			json.Unmarshal(f.Value, &r)
			if r.Records != 2 {
				t.Errorf("records = %d, want 2", r.Records)
			}
			return
		}
	}
	t.Error("no records fact")
}
