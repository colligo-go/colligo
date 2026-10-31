package request

import (
	"context"
	"errors"

	"github.com/colligo-go/colligo/samplesheet"
	"github.com/colligo-go/colligo/secret"
)

// SheetFile resolves an nf-core samplesheet. The reader of samplesheet/ does
// the parsing; this resolver turns its rows into files and hands the sheet on,
// because the pairs it declares are not something names can say.
type SheetFile struct {
	Name secret.Name
}

func (s SheetFile) Resolve(ctx context.Context) (Found, error) {
	sheet, err := samplesheet.Read(s.Name)
	if err != nil {
		var malformed *samplesheet.FormatError
		if errors.As(err, &malformed) {
			return Found{}, usage("%v", err)
		}
		return Found{}, err
	}
	out := Found{Sheet: &sheet}
	for _, row := range sheet.Rows {
		for _, ref := range []samplesheet.Reference{row.Fastq1, row.Fastq2} {
			if ref.Set() && !ref.Remote() {
				out.Files = addFile(out.Files, File{Name: ref.Name(), Origins: []Origin{{Kind: FromSheetLine, Line: row.Line}}})
			}
		}
	}
	return out, nil
}
