// Package manifest produces the portable evidence of a check: the
// certificate a third party can verify without any Colligo code.
package manifest

import (
	"encoding/json"

	"github.com/colligo-go/colligo/facts"
	"github.com/colligo-go/colligo/internal/buildinfo"
	"github.com/colligo-go/colligo/internal/fsaccess"
	"github.com/colligo-go/colligo/model"
	"github.com/colligo-go/colligo/verifier"
)

const (
	CertificateSchemaVersion = 1
	CertificateSuffix        = ".colligo-cert.json"
)

type Tool struct {
	Version string `json:"version"`
	GitSHA  string `json:"git_sha"`
}

type Certificate struct {
	SchemaVersion int               `json:"schema_version"`
	Tool          Tool              `json:"tool"`
	Size          int64             `json:"size"`
	BLAKE3Root    string            `json:"blake3_root"`
	GroupSize     int64             `json:"group_size"`
	MD5           string            `json:"md5"`
	SHA256        string            `json:"sha256"`
	Facts         []model.Fact      `json:"facts"`
	Assertions    []model.Assertion `json:"assertions"`
}

func FromReport(f *verifier.FileReport) Certificate {
	return Certificate{
		SchemaVersion: CertificateSchemaVersion,
		Tool:          Tool{Version: buildinfo.Version(), GitSHA: buildinfo.GitSHA()},
		Size:          f.Size,
		BLAKE3Root:    f.BLAKE3,
		GroupSize:     facts.GroupSize,
		MD5:           f.MD5,
		SHA256:        f.SHA256,
		Facts:         f.Result.Facts,
		Assertions:    f.Assertions,
	}
}

// Certifiable reports whether the file was read in full, which is what a
// certificate attests.
func Certifiable(f *verifier.FileReport) bool {
	return f.Status != verifier.StatusUnreadable && f.Status != verifier.StatusQuick && f.BLAKE3 != ""
}

func Write(f *verifier.FileReport) error {
	out, err := fsaccess.Create(f.Name.Raw, CertificateSuffix)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(out)
	enc.SetIndent("", "  ")
	if err := enc.Encode(FromReport(f)); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}
