package ena

import (
	"bytes"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"

	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
)

var requiredColumns = []string{
	"run_accession",
	"library_layout",
	"fastq_ftp",
	"fastq_md5",
	"fastq_bytes",
	"read_count",
	"base_count",
	"status",
}

var (
	md5Pattern  = regexp.MustCompile(`^[0-9a-f]{32}$`)
	matePattern = regexp.MustCompile(`_[12]\.f(ast)?q\.gz$`)
)

type run struct {
	accession string
	layout    string
	status    string
	counts    model.RunCounts
	artifacts []model.Artifact
}

func parseReport(body []byte) ([]run, []model.ValidationIssue, error) {
	lines := bytes.Split(body, []byte("\n"))
	header := strings.TrimRight(string(lines[0]), "\r")
	if strings.TrimSpace(string(body)) == "" {
		return nil, nil, &model.SourceError{Source: sourceID, Kind: model.SourceEmptyResponse, Detail: "the response body is empty"}
	}
	if !strings.Contains(header, "\t") {
		return nil, nil, &model.SourceError{Source: sourceID, Kind: model.SourceNotTabular, Detail: "the first line has no tab separator"}
	}
	index, err := columnIndex(strings.Split(header, "\t"))
	if err != nil {
		return nil, nil, err
	}

	var runs []run
	var issues []model.ValidationIssue
	seen := map[string]bool{}
	for i, line := range lines[1:] {
		text := strings.TrimRight(string(line), "\r")
		if text == "" {
			continue
		}
		subject := fmt.Sprintf("line %d", i+2)
		cells := strings.Split(text, "\t")
		if len(cells) != len(index.names) {
			issues = append(issues, blocking("ragged_row", subject, fmt.Sprintf("row has %d fields, the header has %d", len(cells), len(index.names))))
			continue
		}
		fields := index.pick(cells)
		if accession := fields["run_accession"]; accession != "" {
			subject = accession
			if seen[accession] {
				issues = append(issues, blocking("duplicate_run", subject, "the run appears in more than one row"))
				continue
			}
			seen[accession] = true
		}
		parsed, rowIssues := parseRow(subject, fields)
		if len(rowIssues) > 0 {
			issues = append(issues, rowIssues...)
			continue
		}
		runs = append(runs, parsed)
	}
	return runs, issues, nil
}

type columns struct {
	names    []string
	position map[string]int
}

func columnIndex(names []string) (columns, error) {
	c := columns{names: names, position: map[string]int{}}
	for i, name := range names {
		c.position[strings.TrimSpace(name)] = i
	}
	var missing []string
	for _, required := range requiredColumns {
		if _, ok := c.position[required]; !ok {
			missing = append(missing, required)
		}
	}
	if len(missing) > 0 {
		return columns{}, &model.SourceError{
			Source: sourceID,
			Kind:   model.SourceMissingColumn,
			Detail: "the header lacks " + strings.Join(missing, ", "),
		}
	}
	return c, nil
}

func (c columns) pick(cells []string) map[string]string {
	fields := make(map[string]string, len(requiredColumns))
	for _, name := range requiredColumns {
		fields[name] = strings.TrimSpace(cells[c.position[name]])
	}
	return fields
}

func parseRow(subject string, fields map[string]string) (run, []model.ValidationIssue) {
	var issues []model.ValidationIssue
	for _, name := range requiredColumns {
		if fields[name] == "" {
			issues = append(issues, blocking("empty_field", subject, name+" is empty"))
		}
	}
	if len(issues) > 0 {
		return run{}, issues
	}

	readCount, err := parseCount(fields["read_count"])
	if err != nil {
		issues = append(issues, blocking("bad_number", subject, "read_count is not a non-negative integer"))
	}
	baseCount, err := parseCount(fields["base_count"])
	if err != nil {
		issues = append(issues, blocking("bad_number", subject, "base_count is not a non-negative integer"))
	}

	urls := strings.Split(fields["fastq_ftp"], ";")
	sums := strings.Split(fields["fastq_md5"], ";")
	sizes := strings.Split(fields["fastq_bytes"], ";")
	if len(urls) != len(sums) || len(urls) != len(sizes) {
		issues = append(issues, blocking("misaligned_lists", subject, fmt.Sprintf(
			"fastq_ftp has %d entries, fastq_md5 has %d, fastq_bytes has %d", len(urls), len(sums), len(sizes))))
	}
	if len(issues) > 0 {
		return run{}, issues
	}

	artifacts := make([]model.Artifact, 0, len(urls))
	for i := range urls {
		artifact, artifactIssues := buildArtifact(subject, fields, urls[i], sums[i], sizes[i])
		issues = append(issues, artifactIssues...)
		artifacts = append(artifacts, artifact)
	}
	if len(issues) > 0 {
		return run{}, issues
	}
	return run{
		accession: fields["run_accession"],
		layout:    fields["library_layout"],
		status:    fields["status"],
		counts:    model.RunCounts{ReadCount: readCount, BaseCount: baseCount},
		artifacts: artifacts,
	}, nil
}

func buildArtifact(subject string, fields map[string]string, location, sum, size string) (model.Artifact, []model.ValidationIssue) {
	var issues []model.ValidationIssue
	location = strings.TrimSpace(location)
	sum = strings.TrimSpace(sum)
	size = strings.TrimSpace(size)

	if location == "" {
		issues = append(issues, blocking("empty_field", subject, "fastq_ftp has an empty entry"))
	}
	if !md5Pattern.MatchString(sum) {
		issues = append(issues, blocking("bad_md5", subject, "fastq_md5 has an entry that is not 32 hex digits"))
	}
	byteCount, err := parseCount(size)
	if err != nil {
		issues = append(issues, blocking("bad_number", subject, "fastq_bytes has an entry that is not a non-negative integer"))
	}
	if len(issues) > 0 {
		return model.Artifact{}, issues
	}

	if !strings.Contains(location, "://") {
		location = "https://" + location
	}
	endpoint, err := secret.NewEndpoint(location)
	if err != nil {
		return model.Artifact{}, []model.ValidationIssue{blocking("bad_url", subject, "fastq_ftp has an entry that is not a URL")}
	}
	name := path.Base(strings.SplitN(location, "?", 2)[0])
	pairID := ""
	if matePattern.MatchString(name) {
		pairID = fields["run_accession"]
	}
	return model.Artifact{
		URL:          endpoint,
		Name:         secret.NewName(name),
		Size:         byteCount,
		MD5:          sum,
		Format:       "fastq",
		PairID:       pairID,
		SourceStatus: fields["status"],
	}, nil
}

func parseCount(text string) (int64, error) {
	n, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return 0, err
	}
	if n < 0 {
		return 0, fmt.Errorf("negative")
	}
	return n, nil
}

func blocking(code, subject, message string) model.ValidationIssue {
	return model.ValidationIssue{Severity: model.Blocking, Code: code, Subject: subject, Message: message}
}
