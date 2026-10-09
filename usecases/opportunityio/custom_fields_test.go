package opportunityio

import (
	"bytes"
	"errors"
	"strings"
	"testing"
)

var errDefinitionsDown = errors.New("definitions down")

func ioWithBrokenDefinitions() (*Service, *fakeOppRepo) {
	oppRepo := newFakeOppRepo()
	return NewService(newOppService(oppRepo, "ws1"), &fakeFieldRepo{err: errDefinitionsDown}), oppRepo
}

func TestExportRefusesWhenDefinitionsCannotBeRead(t *testing.T) {
	io, _ := ioWithBrokenDefinitions()
	var buf bytes.Buffer
	if _, err := io.Export("ws1", "pipe1", nil, false, "", &buf); !errors.Is(err, errDefinitionsDown) {
		t.Fatalf("Export() error = %v, want the definitions error", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("Export() wrote %q before failing", buf.String())
	}
}

func TestExportRefusesWithoutADefinitionSource(t *testing.T) {
	io := NewService(newOppService(newFakeOppRepo(), "ws1"), nil)
	var buf bytes.Buffer
	if _, err := io.Export("ws1", "pipe1", nil, false, "", &buf); err == nil {
		t.Fatal("Export() must refuse when it cannot know the custom columns")
	}
}

func TestImportRefusesWhenDefinitionsCannotBeRead(t *testing.T) {
	io, repo := ioWithBrokenDefinitions()
	csvData := "title,stage_id,pipeline_id,custom_field:score\nDeal,stage1,pipe1,10"
	if _, err := io.Import("ws1", strings.NewReader(csvData), ImportOptions{ActorID: importer}); !errors.Is(err, errDefinitionsDown) {
		t.Fatalf("Import() error = %v, want the definitions error", err)
	}
	if len(repo.store) != 0 {
		t.Fatalf("Import() created %d deals without knowing the field types", len(repo.store))
	}
}

func TestImportReportsAnUnknownCustomColumnPerRow(t *testing.T) {
	io, _ := newIO()
	csvData := "title,stage_id,pipeline_id,custom_field:segmento,custom_field:inexistente\nDeal,stage1,pipe1,smb,x"
	report, err := io.Import("ws1", strings.NewReader(csvData), ImportOptions{ActorID: importer})
	if err != nil {
		t.Fatalf("Import() error = %v", err)
	}
	if report.Created != 0 || len(report.Errors) != 1 || !strings.Contains(report.Errors[0].Message, "inexistente") {
		t.Fatalf("report = %+v", report)
	}
}
