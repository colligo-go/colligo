package secret

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"testing"

	"github.com/colligo-go/colligo/internal/fsaccess"
)

const canary = "CANARY-MUST-NOT-LEAK"

func assertNeverLeaks(t *testing.T, label string, v any) {
	t.Helper()
	forms := []string{
		fmt.Sprintf("%v", v),
		fmt.Sprintf("%s", v),
		fmt.Sprintf("%+v", v),
		fmt.Sprintf("%#v", v),
	}
	if b, err := json.Marshal(v); err != nil {
		t.Fatalf("%s: json.Marshal: %v", label, err)
	} else {
		forms = append(forms, string(b))
	}
	for _, f := range forms {
		if strings.Contains(f, canary) {
			t.Errorf("%s: leaked the canary in %q", label, f)
		}
	}
}

func TestSecretNeverLeaks(t *testing.T) {
	assertNeverLeaks(t, "Secret", Secret{v: canary})
}

func TestRestrictedNameNeverLeaks(t *testing.T) {
	assertNeverLeaks(t, "restricted Name", Name{raw: canary, opaque: "op4c0id", restricted: true})
}

func TestPublicNameShowsThePath(t *testing.T) {
	n := NewName("reads.fastq.gz")
	if got := n.String(); got != "reads.fastq.gz" {
		t.Errorf("String() = %q, want %q", got, "reads.fastq.gz")
	}
}

func TestNameRaw(t *testing.T) {
	n := Name{raw: canary, restricted: true, opaque: "op4c0id"}
	if got := fsaccess.Reveal(n.Raw); got != canary {
		t.Errorf("Raw() = %q, want the real path", got)
	}
}

func TestEndpointNeverLeaksQuery(t *testing.T) {
	e, err := NewEndpoint("https://example.org/path?token=" + canary)
	if err != nil {
		t.Fatal(err)
	}
	assertNeverLeaks(t, "Endpoint with a query", e)
}

func TestRestrictedEndpointNeverLeaksThePath(t *testing.T) {
	e, err := NewEndpoint("https://example.org/" + canary + "?x=1")
	if err != nil {
		t.Fatal(err)
	}
	e.restricted = true
	assertNeverLeaks(t, "restricted Endpoint", e)
	if got := e.String(); got != "https://example.org/[REDACTED]" {
		t.Errorf("restricted String() = %q", got)
	}
}

func TestJoinedEndpointHidesItsQuery(t *testing.T) {
	base, err := NewEndpoint("https://mirror.example/api/")
	if err != nil {
		t.Fatal(err)
	}
	joined := base.Join(url.Values{"token": {canary}}, "filereport")
	if got := joined.String(); got != "https://mirror.example/api/filereport" {
		t.Errorf("String = %q", got)
	}
	if joined.u.Query().Get("token") != canary {
		t.Errorf("the query was not set: %q", joined.u.RawQuery)
	}
	assertNeverLeaks(t, "joined Endpoint", joined)
}

func TestJoinedEndpointStaysRestricted(t *testing.T) {
	base := Endpoint{u: &url.URL{Scheme: "https", Host: "private.example", Path: "/" + canary}, restricted: true}
	joined := base.Join(url.Values{"q": {"1"}}, "more")
	if !joined.restricted {
		t.Fatal("the restriction was lost")
	}
	assertNeverLeaks(t, "joined restricted Endpoint", joined)
}

func TestJoinOnTheZeroEndpointIsZero(t *testing.T) {
	if got := (Endpoint{}).Join(url.Values{"a": {"b"}}, "x").String(); got != "" {
		t.Errorf("String = %q", got)
	}
}

func TestEndpointRevealKeepsTheQueryThatStringDrops(t *testing.T) {
	e, err := NewEndpoint("https://example.org/a?api_key=SENTINEL")
	if err != nil {
		t.Fatal(err)
	}
	if got := e.Reveal(); got != "https://example.org/a?api_key=SENTINEL" {
		t.Fatalf("Reveal %q", got)
	}
	if strings.Contains(e.String(), "SENTINEL") {
		t.Fatalf("String leaks the query: %q", e.String())
	}
	if (Endpoint{}).Reveal() != "" {
		t.Fatal("zero Endpoint must reveal an empty string")
	}
}
