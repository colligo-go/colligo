package verifier

import (
	"context"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"golang.org/x/sync/errgroup"

	"github.com/colligo-go/colligo/diagnoses"
	"github.com/colligo-go/colligo/facts"
	"github.com/colligo-go/colligo/format"
	"github.com/colligo-go/colligo/internal/fsaccess"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/secret"
)

type Options struct {
	Format    string
	Expected  Expected
	Structure bool
	// Quick reads the magic bytes, the tail and a sample of the start. Its
	// result is never verified.
	Quick bool
	Jobs  int
}

const defaultJobs = 2

func contentFormat(declared string) string {
	if v, ok := format.Lookup(declared); ok && !v.Compressed() {
		return declared
	}
	return ""
}

func newReport(name secret.Name, passStatus string) *FileReport {
	return &FileReport{Name: name, passStatus: passStatus}
}

// CheckFile returns an error only for cancellation: every other failure
// becomes an assertion.
func CheckFile(ctx context.Context, name secret.Name, opts Options) (*FileReport, error) {
	if opts.Quick {
		return checkQuick(ctx, name, opts)
	}
	pass := StatusVerified
	if opts.Structure {
		pass = StatusValid
	}
	rep := newReport(name, pass)
	f, err := fsaccess.Open(name.Raw)
	if err != nil {
		return unreadable(rep, err), nil
	}
	defer f.Close()
	return checkSource(ctx, rep, f, opts)
}

type readSource interface {
	io.Reader
	Stat() (os.FileInfo, error)
}

func checkSource(ctx context.Context, rep *FileReport, f readSource, opts Options) (*FileReport, error) {
	info, err := f.Stat()
	if err != nil {
		return unreadable(rep, statFailure(err)), nil
	}
	before := stateOf(info)
	res, err := facts.Compute(ctx, f, facts.Options{Format: contentFormat(opts.Format), Hashes: !opts.Structure})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return unreadable(rep, err), nil
	}
	observed, err := observedChange(rep.Name, f, before)
	if err != nil {
		return unreadable(rep, err), nil
	}
	rep.Result = res
	rep.Container, rep.Content, rep.Size = res.Container, res.Content, res.Size
	if observed != "" {
		rep.Assertions = append(
			[]model.Assertion{newAssertion(diagnoses.ChangedDuringRead, false, diagnoses.Values{Declared: "unchanged", Observed: observed})},
			skipped(evaluatedIDs(), diagnoses.ChangedDuringRead)...)
		rep.finalize()
		return rep, nil
	}
	exp := opts.Expected
	if opts.Structure {
		exp = Expected{}
	}
	if !opts.Structure {
		rep.BLAKE3, rep.MD5, rep.SHA256 = res.RootHex(), res.MD5Hex(), res.SHA256Hex()
	}
	rep.Assertions = Evaluate(rep.Name, res, exp, opts.Format, !opts.Structure)
	rep.finalize()
	return rep, nil
}

func observedChange(name secret.Name, f readSource, before fileState) (string, error) {
	info, err := f.Stat()
	if err != nil {
		return "", statFailure(err)
	}
	opened := stateOf(info)
	observed := ""
	if changed := before.differences(opened); len(changed) > 0 {
		observed = strings.Join(changed, ", ") + " changed"
	}
	switch pathInfo, err := fsaccess.Stat(name.Raw); {
	case errors.Is(err, fs.ErrNotExist):
		observed = "removed"
	case err != nil:
		return "", err
	case !opened.sameFile(stateOf(pathInfo)):
		observed = "replaced"
	}
	return observed, nil
}

func evaluatedIDs() []string {
	var ids []string
	for _, id := range AssertionIDs() {
		switch id {
		case diagnoses.ReadFailed, diagnoses.ChangedDuringRead, diagnoses.PairCount, diagnoses.PairIDs, diagnoses.PairMissing:
		default:
			ids = append(ids, id)
		}
	}
	return ids
}

func unreadable(rep *FileReport, err error) *FileReport {
	rep.Status = StatusUnreadable
	rep.Assertions = []model.Assertion{newAssertion(diagnoses.ReadFailed, false, diagnoses.Values{Detail: err.Error()})}
	return rep
}

func CheckAll(ctx context.Context, names []secret.Name, opts Options) (Report, error) {
	jobs := opts.Jobs
	if jobs <= 0 {
		jobs = defaultJobs
	}
	reports := make([]*FileReport, len(names))
	g, gctx := errgroup.WithContext(ctx)
	g.SetLimit(jobs)
	for i, name := range names {
		g.Go(func() error {
			rep, err := CheckFile(gctx, name, opts)
			reports[i] = rep
			return err
		})
	}
	if err := g.Wait(); err != nil {
		return Report{}, err
	}
	if !opts.Structure && !opts.Quick {
		EvaluatePairs(reports)
	}
	return Report{Files: reports, Quick: opts.Quick}, nil
}

const quickSample = 64 << 10

func checkQuick(ctx context.Context, name secret.Name, opts Options) (*FileReport, error) {
	rep := newReport(name, StatusQuick)
	f, err := fsaccess.Open(name.Raw)
	if err != nil {
		return unreadable(rep, err), nil
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return unreadable(rep, err), nil
	}
	head := make([]byte, min(info.Size(), quickSample))
	if _, err := io.ReadFull(f, head); err != nil {
		return unreadable(rep, err), nil
	}
	res := facts.Result{Size: info.Size()}
	res.Container, res.Content = format.Sniff(head)
	if opts.Format != "" {
		if v, ok := format.Lookup(opts.Format); ok && !v.Compressed() && res.Content != format.ContentHTML {
			res.Content = opts.Format
		}
	}
	if res.Container == format.ContainerBgzf && info.Size() >= 28 {
		tail := make([]byte, 28)
		if _, err := f.ReadAt(tail, info.Size()-28); err != nil {
			return unreadable(rep, err), nil
		}
		res.Facts = append(res.Facts, bgzfTailFact(tail))
	}
	if res.Container == format.ContainerNone && res.Content != format.ContentHTML && info.Size() > 0 {
		last := make([]byte, 1)
		if _, err := f.ReadAt(last, info.Size()-1); err != nil {
			return unreadable(rep, err), nil
		}
		res.Facts = append(res.Facts, textTailFact(res.Content, last[0]))
	}
	rep.Result = res
	rep.Container, rep.Content, rep.Size = res.Container, res.Content, res.Size
	rep.Assertions = Evaluate(rep.Name, res, Expected{}, opts.Format, false)
	rep.finalize()
	return rep, ctx.Err()
}

// Discover classifies the files of a directory by content. A file is a
// candidate when its content is a known format or its name says it should
// be one, so a web page saved as .gtf.gz is rejected instead of ignored. A
// file named explicitly is always checked.
func Discover(ctx context.Context, root secret.Name, recursive bool) (names []secret.Name, skipped, dirLinks []string, err error) {
	isDir, err := fsaccess.IsDir(root.Raw)
	if err != nil {
		return nil, nil, nil, err
	}
	if !isDir {
		return []secret.Name{root}, nil, nil, nil
	}
	entries, err := fsaccess.ReadDir(root.Raw)
	if err != nil {
		return nil, nil, nil, err
	}
	for _, e := range entries {
		if err := ctx.Err(); err != nil {
			return nil, nil, nil, err
		}
		name := secret.NewName(e.Path)
		if e.Dir {
			if !recursive || hidden(name) {
				continue
			}
			sub, subSkipped, subLinks, err := Discover(ctx, name, recursive)
			if err != nil {
				return nil, nil, nil, err
			}
			names, skipped, dirLinks = append(names, sub...), append(skipped, subSkipped...), append(dirLinks, subLinks...)
			continue
		}
		if e.DirLink {
			if !hidden(name) {
				dirLinks = append(dirLinks, name.String())
			}
			continue
		}
		if hidden(name) {
			continue
		}
		if looksLikeData(name) {
			names = append(names, name)
		} else {
			skipped = append(skipped, name.String())
		}
	}
	sort.Strings(skipped)
	sort.Strings(dirLinks)
	return names, skipped, dirLinks, nil
}

func hidden(name secret.Name) bool {
	return strings.HasPrefix(filepath.Base(name.String()), ".")
}

func looksLikeData(name secret.Name) bool {
	f, err := fsaccess.Open(name.Raw)
	if err != nil {
		return true
	}
	defer f.Close()
	head := make([]byte, quickSample)
	n, _ := io.ReadFull(f, head)
	container, content := format.Sniff(head[:n])
	if container != format.ContainerNone || content == format.ContentFastq || content == format.ContentFasta || content == format.ContentGTF {
		return true
	}
	nameContainer, nameContent := format.FromName(name)
	return nameContainer != format.ContainerNone || nameContent != ""
}
