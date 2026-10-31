package identity

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/model"
)

const tinyFasta = ">chrA first record\nACGTACGT\nacgtNN\n>chrB\nGGGGCCCC\n\n>chrC desc\nTTTT\n"

const (
	tinyDigest          = "-ILNzNQsKYl_9KDXtkCWYUl5t5NESUdA"
	tinyNamesDigest     = "CAGy-sVTWUrV8SJomv02N6iVn0ro28ok"
	tinyLengthsDigest   = "wcAyh-wBAWVP_j2o0s4WCpXCggoBggXN"
	tinySequencesDigest = "ovIi3RsJDtIbAV_4X6Fi4D_PGB8hclgB"
)

func collect(t *testing.T, text string) Collection {
	t.Helper()
	c := NewCollector()
	if _, err := c.Write([]byte(text)); err != nil {
		t.Fatal(err)
	}
	got, err := c.Collection()
	if err != nil {
		t.Fatal(err)
	}
	return got
}

func digestOf(t *testing.T, text string) Digests {
	t.Helper()
	d, err := collect(t, text).Digest()
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSha512t24uMatchesRefgetVectors(t *testing.T) {
	cases := map[string]string{
		"ACGT": "aKF498dAxcJAqme6QYQ7EZ07-fiw8Kw2",
		"":     "z4PhNX7vuL3xVChQ1m2AB9Yg5AULVxXc",
	}
	for input, want := range cases {
		if got := Sha512t24u([]byte(input)); got != want {
			t.Errorf("Sha512t24u(%q) = %s, want %s", input, got, want)
		}
	}
}

func TestDigestReproducesSpecificationExample(t *testing.T) {
	c := Collection{
		Names:   []string{"chr1", "chr2", "chr3"},
		Lengths: []int64{248956422, 242193529, 198295559},
		Sequences: []string{
			"SQ.2YnepKM7OkBoOrKmvHbGqguVfF9amCST",
			"SQ.lwDyBi432Py-7xnAISyQlnlhWDEaBPv2",
			"SQ.Eqk6_SvMMDCc6C-uEfickOUWTatLMDQZ",
		},
	}
	d, err := c.Digest()
	if err != nil {
		t.Fatal(err)
	}
	want := Digests{
		Collection: "sjNNwm4zov3Dl0FRWbRTcZwzqrTQKIqL",
		Labels:     "g04lKdxiYtG3dOGeUC5AdKEifw65G0Wp",
		Lengths:    "5K4odB173rjao1Cnbk5BnvLt9V7aPAa2",
		Sequences:  "rD29ZKmEqwwHRXjiQ36p6UMZQ5hemmsb",
	}
	if d != want {
		t.Errorf("digests = %+v, want %+v", d, want)
	}
}

func TestDigestReproducesLevelZeroExample(t *testing.T) {
	c := Collection{
		Names:   []string{"A", "B", "C"},
		Lengths: []int64{1216, 970, 1788},
		Sequences: []string{
			"SQ.OL3sVAcd_5IZaDxUkH-yQkLmBz2iwY0s",
			"SQ.kny8cdhEEPHXoNlXmps8NQapGtUKZlM9",
			"SQ.DA-GLdXVihnYKs-fBS5MMgqMi7tVMJbt",
		},
	}
	d, err := c.Digest()
	if err != nil {
		t.Fatal(err)
	}
	if d.Collection != "Zjx9_tD2o-1yKB6RR2v2g3W9c5ufydUc" || d.Labels != "1zOnTYE5slcISev72o62ySxbssEXeoUL" ||
		d.Lengths != "QWhPI-Cll_0Y5NJ_2krRryuV97vzhbgJ" || d.Sequences != "uPCc00rq-daL3zPnzYH-sBg9_z7HpB8B" {
		t.Errorf("digests = %+v", d)
	}
}

func TestLengthsDoNotContributeToTheCollectionDigest(t *testing.T) {
	a := Collection{Names: []string{"x"}, Lengths: []int64{1}, Sequences: []string{"SQ.aKF498dAxcJAqme6QYQ7EZ07-fiw8Kw2"}}
	b := a
	b.Lengths = []int64{999}
	da, _ := a.Digest()
	db, _ := b.Digest()
	if da.Collection != db.Collection || da.Lengths == db.Lengths {
		t.Errorf("collection %s/%s lengths %s/%s", da.Collection, db.Collection, da.Lengths, db.Lengths)
	}
}

func TestCollectorMatchesIndependentImplementation(t *testing.T) {
	got := digestOf(t, tinyFasta)
	want := Digests{Collection: tinyDigest, Labels: tinyNamesDigest, Lengths: tinyLengthsDigest, Sequences: tinySequencesDigest}
	if got != want {
		t.Errorf("digests = %+v, want %+v", got, want)
	}
}

func TestDigestIgnoresLineWidthCaseLineEndingsAndHeaderText(t *testing.T) {
	variants := map[string]string{
		"crlf":      strings.ReplaceAll(tinyFasta, "\n", "\r\n"),
		"one line":  ">chrA\nACGTACGTACGTNN\n>chrB\nGGGGCCCC\n>chrC\nTTTT\n",
		"narrow":    ">chrA x\nAC\nGT\nAC\nGT\nAC\nGT\nNN\n>chrB y\nGG\nGG\nCC\nCC\n>chrC z\nT\nT\nT\nT\n",
		"lowercase": ">chrA\nacgtacgtacgtnn\n>chrB\ngggg\ncccc\n>chrC\ntttt\n",
	}
	for name, text := range variants {
		if got := digestOf(t, text).Collection; got != tinyDigest {
			t.Errorf("%s: digest = %s, want %s", name, got, tinyDigest)
		}
	}
}

func TestOneChangedBaseChangesTheDigest(t *testing.T) {
	changed := strings.Replace(tinyFasta, "GGGGCCCC", "GGGGCCCA", 1)
	got := digestOf(t, changed)
	if got.Collection != "N0YRD0n4wtA3-ThKhQSX4fCW0--XU5U0" || got.Collection == tinyDigest {
		t.Errorf("digest = %s", got.Collection)
	}
	if got.Labels != tinyNamesDigest || got.Lengths != tinyLengthsDigest {
		t.Errorf("only the sequences digest should move: %+v", got)
	}
}

func TestCollectorGivesTheSameResultForEveryChunking(t *testing.T) {
	for _, size := range []int{1, 2, 3, 7, 16, len(tinyFasta)} {
		c := NewCollector()
		for rest := []byte(tinyFasta); len(rest) > 0; {
			n := min(size, len(rest))
			c.Write(rest[:n])
			rest = rest[n:]
		}
		got, err := c.Collection()
		if err != nil {
			t.Fatal(err)
		}
		d, _ := got.Digest()
		if d.Collection != tinyDigest {
			t.Errorf("chunk size %d: digest = %s", size, d.Collection)
		}
	}
}

func TestCollectorHandlesALineLongerThanTheScratchBuffer(t *testing.T) {
	long := strings.Repeat("ACGT", scratchSize)
	split := ">big\n" + long + "\n"
	one := ">big\n" + strings.Repeat("ACGT", scratchSize/2) + "\n" + strings.Repeat("ACGT", scratchSize/2) + "\n"
	if digestOf(t, split).Collection != digestOf(t, one).Collection {
		t.Error("a very long line digests differently from the same bases wrapped")
	}
	if got := collect(t, split).Lengths[0]; got != int64(4*scratchSize) {
		t.Errorf("length = %d", got)
	}
}

func TestCanonicalNamesFollowRFC8785(t *testing.T) {
	names := []string{"a\"b", "c\\d", "e\x01f", "g\x7fh", "é∑"}
	got, err := canonicalStrings(names)
	if err != nil {
		t.Fatal(err)
	}
	if Sha512t24u(got) != "3S4FactEAHBlY6laxK8zE1OVWWm-xxD3" {
		t.Errorf("canonical form %q", got)
	}
}

func TestInvalidUTF8NameCannotBeDigested(t *testing.T) {
	c := Collection{Names: []string{"bad\xff"}, Lengths: []int64{1}, Sequences: []string{"SQ.x"}}
	if _, err := c.Digest(); err == nil {
		t.Error("expected an error for a name that is not UTF-8")
	}
}

func TestCollectorRefusesWhatHasNoCollection(t *testing.T) {
	cases := map[string]string{
		"empty file":     "",
		"no header":      "ACGT\nACGT\n",
		"nameless":       ">\nACGT\n",
		"space only":     "> \nACGT\n",
		"name too long":  ">" + strings.Repeat("n", maxNameBytes+1) + "\nACGT\n",
		"before headers": "ACGT\n",
	}
	for name, text := range cases {
		c := NewCollector()
		c.Write([]byte(text))
		if _, err := c.Collection(); err == nil {
			t.Errorf("%s: expected an error", name)
		}
	}
}

func TestCollectorBoundsTheTotalSizeOfTheNames(t *testing.T) {
	c := NewCollector()
	c.namesCap = 10
	c.Write([]byte(">abcd\nAC\n>efgh\nAC\n>ijkl\nAC\n"))
	if _, err := c.Collection(); !errors.Is(err, errNamesTooLarge) {
		t.Fatalf("err = %v, want the names budget error", err)
	}
	fact, err := c.Fact()
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fact.Value), "256 MiB") || strings.Contains(string(fact.Value), "digest") {
		t.Errorf("fact = %s", fact.Value)
	}
}

func TestCollectorAcceptsNamesUpToTheBudget(t *testing.T) {
	c := NewCollector()
	c.namesCap = 8
	c.Write([]byte(">abcd\nAC\n>efgh\nAC\n"))
	if _, err := c.Collection(); err != nil {
		t.Fatalf("err = %v", err)
	}
}

func TestEmptySequenceIsARecordOfLengthZero(t *testing.T) {
	got := collect(t, ">a\n>b\nAC\n")
	if len(got.Names) != 2 || got.Lengths[0] != 0 || got.Lengths[1] != 2 || got.Sequences[0] != "SQ.z4PhNX7vuL3xVChQ1m2AB9Yg5AULVxXc" {
		t.Errorf("collection = %+v", got)
	}
}

func TestFactCarriesTheDigestOrTheReason(t *testing.T) {
	c := NewCollector()
	c.Write([]byte(tinyFasta))
	fact, err := c.Fact()
	if err != nil {
		t.Fatal(err)
	}
	var value SeqcolValue
	if err := json.Unmarshal(fact.Value, &value); err != nil {
		t.Fatal(err)
	}
	if fact.Kind != "seqcol" || fact.ExtractorVersion != ExtractorVersion || value.Digest != tinyDigest || value.Sequences != 3 || value.Reason != "" {
		t.Errorf("fact = %+v value = %+v", fact, value)
	}
	empty := NewCollector()
	fact, err = empty.Fact()
	if err != nil {
		t.Fatal(err)
	}
	value = SeqcolValue{}
	json.Unmarshal(fact.Value, &value)
	if value.Digest != "" || value.Reason == "" {
		t.Errorf("value = %+v", value)
	}
}

func TestWriteDoesNotAllocateOnTheSequencePath(t *testing.T) {
	c := NewCollector()
	c.Write([]byte(">chr\n"))
	line := []byte(strings.Repeat("ACGTNacgtn", 600) + "\n")
	c.Write(line)
	if allocs := testing.AllocsPerRun(100, func() { c.Write(line) }); allocs != 0 {
		t.Errorf("allocations per Write = %v", allocs)
	}
}

func TestEveryKnownReferenceIsComplete(t *testing.T) {
	seen := map[string]bool{}
	for _, r := range Known() {
		if r.Label == "" || r.Source == "" || len(r.Digest) != 32 {
			t.Errorf("incomplete reference %+v", r)
		}
		if seen[r.Digest] {
			t.Errorf("duplicate digest %s", r.Digest)
		}
		seen[r.Digest] = true
	}
}

func TestRecognizeMatchesOnlyTheExactDigest(t *testing.T) {
	table := []Reference{{Label: "tiny", Digest: tinyDigest, Source: "test"}}
	if r, ok := Recognize(tinyDigest, table); !ok || r.Label != "tiny" {
		t.Errorf("Recognize = %+v, %v", r, ok)
	}
	if _, ok := Recognize("N0YRD0n4wtA3-ThKhQSX4fCW0--XU5U0", table); ok {
		t.Error("a different digest was recognized")
	}
	if _, ok := Recognize(tinyDigest, nil); ok {
		t.Error("an empty table recognized something")
	}
}

func TestVerifyPassesAndFailsLoudlyWhenSabotaged(t *testing.T) {
	if err := Verify(); err != nil {
		t.Fatal(err)
	}
	saved := vectors[1].want
	vectors[1].want = strings.Repeat("A", 32)
	defer func() { vectors[1].want = saved }()
	var selfTest *SelfTestError
	if err := Verify(); !errors.As(err, &selfTest) || selfTest.Input != "ACGT" || selfTest.Severity() != model.Fatal {
		t.Errorf("Verify() = %v", err)
	}
}
