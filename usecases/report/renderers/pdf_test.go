package report_renderers

import (
	"context"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
	"testing"

	"vozko/domain/report"
	"vozko/infra/browser"
)

type printerStub struct {
	available bool
	url       string
	data      []byte
	err       error
}

func (p *printerStub) Available() bool { return p.available }

func (p *printerStub) Render(_ context.Context, target string, _ browser.PDFOptions) ([]byte, error) {
	p.url = target
	if p.err != nil {
		return nil, p.err
	}
	return p.data, nil
}

const pdfSecret = "a-secret-that-is-long-enough"

func pdfJob(locale string) report.Job {
	return report.Job{
		ID:          "job-1",
		WorkspaceID: "ws-1",
		Kind:        report.KindAttendanceOverview,
		Format:      report.FormatPDF,
		Locale:      locale,
		Params:      json.RawMessage(`{"dateFrom":"2026-09-01","dateTo":"2026-09-23"}`),
	}
}

func TestPDFRendererBuildsALocalisedPrintURL(t *testing.T) {
	printer := &printerStub{available: true, data: []byte("%PDF-1.4")}
	renderer := NewPDFRenderer(
		report.KindAttendanceOverview, printer,
		"https://app.example.com/", pdfSecret, "atendimento",
	)

	artifact, err := renderer.Render(context.Background(), pdfJob("en"), func(int) {})
	if err != nil {
		t.Fatalf("render: %v", err)
	}

	parsed, err := url.Parse(printer.url)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	if parsed.Path != "/en/print/report/job-1" {
		t.Fatalf("path = %q, want the locale-prefixed print route", parsed.Path)
	}
	if parsed.Query().Get("token") == "" {
		t.Fatal("the print URL carries no token")
	}
	if artifact.ContentType != "application/pdf" {
		t.Fatalf("content type = %q", artifact.ContentType)
	}
	if !strings.HasSuffix(artifact.Filename, ".pdf") {
		t.Fatalf("filename = %q", artifact.Filename)
	}
}

func TestPDFRendererFallsBackToTheDefaultLocale(t *testing.T) {
	for _, locale := range []string{"", "fr", "pt-BR"} {
		printer := &printerStub{available: true, data: []byte("%PDF-1.4")}
		renderer := NewPDFRenderer(
			report.KindAttendanceOverview, printer,
			"https://app.example.com", pdfSecret, "atendimento",
		)
		if _, err := renderer.Render(context.Background(), pdfJob(locale), func(int) {}); err != nil {
			t.Fatalf("render %q: %v", locale, err)
		}

		parsed, _ := url.Parse(printer.url)
		want := "/pt/print/report/job-1"
		if locale == "pt-BR" {
			want = "/pt/print/report/job-1"
		}
		if parsed.Path != want {
			t.Fatalf("locale %q gave path %q, want %q", locale, parsed.Path, want)
		}
	}
}

func TestPDFRendererMintsATokenTheServerAccepts(t *testing.T) {
	printer := &printerStub{available: true, data: []byte("%PDF-1.4")}
	renderer := NewPDFRenderer(
		report.KindAttendanceOverview, printer,
		"https://app.example.com", pdfSecret, "atendimento",
	)
	job := pdfJob("pt")

	if _, err := renderer.Render(context.Background(), job, func(int) {}); err != nil {
		t.Fatalf("render: %v", err)
	}

	parsed, _ := url.Parse(printer.url)
	grant, err := report.VerifyPrintToken(pdfSecret, parsed.Query().Get("token"), renderer.now())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if grant.JobID != job.ID || grant.WorkspaceID != job.WorkspaceID {
		t.Fatalf("grant = %+v, want it scoped to this job alone", grant)
	}
}

func TestPDFRendererRefusesWithoutABrowser(t *testing.T) {
	renderer := NewPDFRenderer(
		report.KindAttendanceOverview, &printerStub{available: false},
		"https://app.example.com", pdfSecret, "atendimento",
	)

	_, err := renderer.Render(context.Background(), pdfJob("pt"), func(int) {})
	if !errors.Is(err, report.ErrNoRenderer) {
		t.Fatalf("err = %v, want ErrNoRenderer", err)
	}
}

func TestPDFRendererRefusesWithoutAPrintBase(t *testing.T) {
	renderer := NewPDFRenderer(
		report.KindAttendanceOverview, &printerStub{available: true},
		"  ", pdfSecret, "atendimento",
	)

	if _, err := renderer.Render(context.Background(), pdfJob("pt"), func(int) {}); err == nil {
		t.Fatal("a renderer with no print base must fail rather than fetch a bad URL")
	}
}

func TestFormatRouterSendsEachFormatToItsRenderer(t *testing.T) {
	csv := &stubRenderer{
		kind:     report.KindAttendanceOverview,
		formats:  []report.Format{report.FormatCSV},
		artifact: report.Artifact{Data: []byte("a;b")},
	}
	pdf := &stubRenderer{
		kind:     report.KindAttendanceOverview,
		formats:  []report.Format{report.FormatPDF},
		artifact: report.Artifact{Data: []byte("%PDF")},
	}

	router := report.NewFormatRouter(report.KindAttendanceOverview, csv, pdf)

	if _, err := router.Render(context.Background(),
		report.Job{Format: report.FormatCSV}, func(int) {}); err != nil {
		t.Fatalf("csv: %v", err)
	}
	if csv.calls != 1 || pdf.calls != 0 {
		t.Fatalf("csv=%d pdf=%d", csv.calls, pdf.calls)
	}

	if _, err := router.Render(context.Background(),
		report.Job{Format: report.FormatPDF}, func(int) {}); err != nil {
		t.Fatalf("pdf: %v", err)
	}
	if pdf.calls != 1 {
		t.Fatalf("pdf ran %d times", pdf.calls)
	}

	if _, err := router.Render(context.Background(),
		report.Job{Format: report.FormatXLSX}, func(int) {}); !errors.Is(err, report.ErrFormatUnsupported) {
		t.Fatalf("err = %v, want ErrFormatUnsupported", err)
	}
}

type stubRenderer struct {
	kind     report.Kind
	formats  []report.Format
	artifact report.Artifact
	calls    int
}

func (r *stubRenderer) Kind() report.Kind        { return r.kind }
func (r *stubRenderer) Formats() []report.Format { return r.formats }
func (r *stubRenderer) Render(context.Context, report.Job, report.ProgressFunc) (report.Artifact, error) {
	r.calls++
	return r.artifact, nil
}
