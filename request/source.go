package request

import (
	"fmt"
	"net/url"
	"path/filepath"
	"regexp"
	"strings"
)

const sourceENA = "ena"

var (
	accessionShape = regexp.MustCompile(`^[A-Z]{2,6}[0-9]+$`)
	runInFileName  = regexp.MustCompile(`^([EDS]RR[0-9]+)(?:[._-]|$)`)
	enaHosts       = []string{"www.ebi.ac.uk", "ebi.ac.uk"}
)

// ParseSource reads the value of --source. It accepts ena:<accession> in any
// case, an ENA address copied from a browser, and the bare word ena, which
// deduces the accession from the files when they all belong to one run. Each
// reading is recorded in the Provenance of the result, so the report can say
// how the accession was obtained.
func ParseSource(value string, files []File) (SourceRef, error) {
	value = strings.TrimSpace(value)
	if looksLikeAddress(value) {
		return fromLink(value)
	}
	id, accession, hasAccession := strings.Cut(value, ":")
	id = strings.ToLower(strings.TrimSpace(id))
	accession = strings.TrimSpace(accession)
	switch {
	case id == "":
		return SourceRef{}, usage("--source takes ena:<accession>, an ENA address, or ena alone to deduce the accession from the file names")
	case id != sourceENA:
		return SourceRef{}, usage("unknown source %q; the only source today is ena", id)
	case !hasAccession:
		return deduce(files)
	case accession == "":
		return SourceRef{}, usage("--source ena: needs an accession after the colon; write --source ena alone to deduce it from the file names")
	}
	ref := SourceRef{ID: sourceENA, Accession: strings.ToUpper(accession)}
	if ref.Accession != accession {
		ref.Provenance = Normalized
	}
	return ref, nil
}

func looksLikeAddress(value string) bool {
	lower := strings.ToLower(value)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return true
	}
	for _, host := range enaHosts {
		if strings.HasPrefix(lower, host+"/") {
			return true
		}
	}
	return false
}

func fromLink(value string) (SourceRef, error) {
	if !strings.Contains(value, "://") {
		value = "https://" + value
	}
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" {
		return SourceRef{}, usage("--source: the address is not valid; copy the ENA address again or write ena:<accession>")
	}
	if !isENAHost(strings.ToLower(u.Hostname())) {
		return SourceRef{}, usage("--source: %s is not an ENA address; the only source today is ena, so give an address of www.ebi.ac.uk/ena or ena:<accession>", u.Hostname())
	}
	if !strings.HasPrefix(u.Path, "/ena/") {
		return SourceRef{}, usage("--source: this EBI address is not a page of the ENA (it must start with www.ebi.ac.uk/ena/); copy the address of the record or write ena:<accession>")
	}
	var found []string
	for _, segment := range strings.Split(u.Path, "/") {
		segment, _, _ = strings.Cut(segment, "&")
		candidate := strings.ToUpper(segment)
		if accessionShape.MatchString(candidate) && !contains(found, candidate) {
			found = append(found, candidate)
		}
	}
	switch len(found) {
	case 0:
		return SourceRef{}, usage("--source: no accession found in the address; open the record of a run or study and copy that address, or write ena:<accession>")
	case 1:
		return SourceRef{ID: sourceENA, Accession: found[0], Provenance: FromLink}, nil
	}
	return SourceRef{}, usage("--source: the address holds more than one accession (%s); write ena:<accession> with the one you mean", strings.Join(found, ", "))
}

func isENAHost(host string) bool { return contains(enaHosts, host) }

func contains(list []string, want string) bool {
	for _, have := range list {
		if have == want {
			return true
		}
	}
	return false
}

func deduce(files []File) (SourceRef, error) {
	if len(files) == 0 {
		return SourceRef{}, usage("--source ena: there are no files to deduce the accession from; give the files or write --source ena:<accession>")
	}
	var runs, unnamed []string
	for _, f := range files {
		base := filepath.Base(f.Name.String())
		match := runInFileName.FindStringSubmatch(base)
		switch {
		case match == nil:
			unnamed = append(unnamed, base)
		case !contains(runs, match[1]):
			runs = append(runs, match[1])
		}
	}
	switch {
	case len(unnamed) > 0:
		return SourceRef{}, usage("--source ena: the accession cannot be deduced because %s carries no run accession (ERR, SRR or DRR followed by digits); write --source ena:<accession>", describeNames(unnamed))
	case len(runs) > 1:
		return SourceRef{}, usage("--source ena: the files belong to %d runs (%s), so Colligo cannot tell which to compare them with. Write --source ena:<accession> with one run, or with the study that holds them all", len(runs), strings.Join(runs, ", "))
	}
	return SourceRef{ID: sourceENA, Accession: runs[0], Provenance: Deduced}, nil
}

func describeNames(names []string) string {
	if len(names) == 1 {
		return names[0]
	}
	shown := names[:min(len(names), 3)]
	out := strings.Join(shown, ", ")
	if len(names) > len(shown) {
		out += ", ..."
	}
	return fmt.Sprintf("%d files (%s)", len(names), out)
}
