package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/colligo-go/colligo/internal/drift"
	"github.com/colligo-go/colligo/source/ena"
	"github.com/colligo-go/colligo/transport"
)

const (
	exitClean   = 0
	exitDrift   = 1
	exitFailure = 2
)

type meta struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
}

type fixture struct {
	name      string
	accession string
	body      []byte
}

func main() {
	fixtures := flag.String("fixtures", "source/ena/testdata", "directory with the recorded ENA responses")
	flag.Parse()
	client, err := transport.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "drift:", err)
		os.Exit(exitFailure)
	}
	os.Exit(run(*fixtures, client, os.Stdout, os.Stderr))
}

func run(dir string, source transport.Transport, stdout, stderr io.Writer) int {
	recorded, err := load(dir)
	if err != nil {
		fmt.Fprintln(stderr, "drift:", err)
		return exitFailure
	}
	live := ena.New(ena.WithTransport(source))

	drifted := 0
	for _, f := range recorded {
		baselineFetched, baselineErr := ena.New(ena.WithTransport(drift.Replay{Body: f.body})).Fetch(context.Background(), f.accession)
		liveFetched, liveErr := live.Fetch(context.Background(), f.accession)
		if drift.IsOutage(liveErr) {
			fmt.Fprintf(stderr, "drift: %s: the source did not answer properly, this is not drift: %v\n", f.name, liveErr)
			return exitFailure
		}

		lines := drift.DiffOutcome(drift.Observe(baselineFetched, baselineErr), drift.Observe(liveFetched, liveErr))
		var notes []string
		if len(liveFetched.Raw) == 1 {
			raw := liveFetched.Raw[0]
			lines = append(lines, drift.DiffEnvelope(raw)...)
			shapeDrift, shapeNotes := drift.Diff(drift.ShapeOf(f.body), drift.ShapeOf(raw.Body))
			lines, notes = append(lines, shapeDrift...), shapeNotes
		}
		if baselineErr == nil && liveErr == nil {
			lines = append(lines, drift.DiffValues(baselineFetched, liveFetched)...)
		}
		if len(lines) == 0 {
			fmt.Fprintf(stdout, "ok     %s\n", f.name)
			for _, note := range notes {
				fmt.Fprintf(stdout, "         note: %s\n", note)
			}
			continue
		}
		drifted++
		fmt.Fprintf(stdout, "DRIFT  %s\n", f.name)
		for _, line := range lines {
			fmt.Fprintf(stdout, "         %s\n", line)
		}
		for _, note := range notes {
			fmt.Fprintf(stdout, "         note: %s\n", note)
		}
	}
	if drifted > 0 {
		fmt.Fprintf(stdout, "\n%d of %d recorded responses no longer match what the source gives today.\n", drifted, len(recorded))
		return exitDrift
	}
	return exitClean
}

func load(dir string) ([]fixture, error) {
	paths, err := filepath.Glob(filepath.Join(dir, "*.tsv.meta.json"))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return nil, fmt.Errorf("no recorded response in %s", dir)
	}
	sort.Strings(paths)
	var fixtures []fixture
	for _, metaPath := range paths {
		f, err := loadOne(metaPath)
		if err != nil {
			return nil, err
		}
		fixtures = append(fixtures, f)
	}
	return fixtures, nil
}

func loadOne(metaPath string) (fixture, error) {
	name := filepath.Base(strings.TrimSuffix(metaPath, ".meta.json"))
	raw, err := os.ReadFile(metaPath)
	if err != nil {
		return fixture{}, err
	}
	var m meta
	if err := json.Unmarshal(raw, &m); err != nil {
		return fixture{}, fmt.Errorf("%s: %w", name, err)
	}
	parsed, err := url.Parse(m.URL)
	if err != nil {
		return fixture{}, fmt.Errorf("%s: the recorded url does not parse", name)
	}
	accession := parsed.Query().Get("accession")
	if accession == "" {
		return fixture{}, fmt.Errorf("%s: the recorded url has no accession", name)
	}
	body, err := os.ReadFile(strings.TrimSuffix(metaPath, ".meta.json"))
	if err != nil {
		return fixture{}, err
	}
	sum := sha256.Sum256(body)
	if hex.EncodeToString(sum[:]) != m.SHA256 {
		return fixture{}, fmt.Errorf("%s: the recorded body does not match the sha256 in its meta file", name)
	}
	return fixture{name: name, accession: accession, body: body}, nil
}
