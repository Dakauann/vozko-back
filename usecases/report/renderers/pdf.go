package report_renderers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"vozko/domain/report"
	"vozko/infra/browser"
)

type PagePrinter interface {
	Render(ctx context.Context, url string, options browser.PDFOptions) ([]byte, error)
	Available() bool
}

type PDFRenderer struct {
	kind        report.Kind
	printer     PagePrinter
	printBase   string
	printSecret string
	filename    string
	now         func() time.Time
}

func NewPDFRenderer(
	kind report.Kind,
	printer PagePrinter,
	printBase string,
	printSecret string,
	filename string,
) *PDFRenderer {
	return &PDFRenderer{
		kind:        kind,
		printer:     printer,
		printBase:   strings.TrimRight(strings.TrimSpace(printBase), "/"),
		printSecret: printSecret,
		filename:    filename,
		now:         func() time.Time { return time.Now().UTC() },
	}
}

var printLocales = map[string]bool{"pt": true, "en": true, "es": true, "de": true}

const defaultPrintLocale = "pt"

func printLocale(locale string) string {
	normalized := strings.ToLower(strings.TrimSpace(locale))
	if index := strings.IndexAny(normalized, "-_"); index > 0 {
		normalized = normalized[:index]
	}
	if printLocales[normalized] {
		return normalized
	}
	return defaultPrintLocale
}

func (r *PDFRenderer) Kind() report.Kind { return r.kind }

func (r *PDFRenderer) Formats() []report.Format {
	return []report.Format{report.FormatPDF}
}

func (r *PDFRenderer) Render(
	ctx context.Context,
	job report.Job,
	progress report.ProgressFunc,
) (report.Artifact, error) {
	if r.printer == nil || !r.printer.Available() {
		return report.Artifact{}, report.ErrNoRenderer
	}
	if r.printBase == "" {
		return report.Artifact{}, fmt.Errorf("pdf report: the print base URL is not configured")
	}

	token, err := report.NewPrintToken(r.printSecret, report.PrintGrant{
		JobID:       job.ID,
		WorkspaceID: job.WorkspaceID,
		ExpiresAt:   r.now().Add(report.PrintTokenTTL),
	})
	if err != nil {
		return report.Artifact{}, err
	}

	query := url.Values{}
	query.Set("token", token)

	printURL := fmt.Sprintf("%s/%s/print/report/%s?%s",
		r.printBase, printLocale(job.Locale), url.PathEscape(job.ID), query.Encode())

	progress(15)

	data, err := r.printer.Render(ctx, printURL, browser.A4Portrait())
	if err != nil {
		return report.Artifact{}, err
	}

	progress(95)

	var params AttendanceParams
	_ = json.Unmarshal(job.Params, &params)

	return report.Artifact{
		Data:        data,
		ContentType: report.FormatPDF.ContentType(),
		Filename:    report.Filename("pdf", r.filename, params.DateFrom, params.DateTo),
	}, nil
}
