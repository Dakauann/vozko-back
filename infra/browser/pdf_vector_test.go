package browser

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	pdfreader "github.com/ledongthuc/pdf"
)

const probePage = `<!doctype html>
<html><head><meta charset="utf-8"><style>
  body { font-family: Arial, sans-serif; padding: 40px; }
  h1 { font-size: 28px; }
</style></head>
<body>
  <h1>Relatorio de atendimento</h1>
  <p>Conversas encerradas no periodo: 1538</p>
  <svg width="300" height="120" viewBox="0 0 300 120">
    <rect x="10" y="20" width="120" height="80" fill="#00D09A"></rect>
    <text x="150" y="70" font-family="Arial" font-size="14">Concluidas 66,7%</text>
  </svg>
  <script>window.__REPORT_READY__ = true;</script>
</body></html>`

func TestPrintedPDFKeepsTextAsTextNotPixels(t *testing.T) {
	if locateChromium() == "" {
		t.Skip("no chromium executable on this machine")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, probePage)
	}))
	defer server.Close()

	data, err := NewRenderer().Render(context.Background(), server.URL, A4Portrait())
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	if !bytes.HasPrefix(data, []byte("%PDF")) {
		t.Fatalf("output is not a PDF: %q", data[:min(8, len(data))])
	}

	file, err := os.CreateTemp(t.TempDir(), "probe-*.pdf")
	if err != nil {
		t.Fatalf("temp: %v", err)
	}
	if _, err := file.Write(data); err != nil {
		t.Fatalf("write: %v", err)
	}
	_ = file.Close()

	handle, reader, err := pdfreader.Open(file.Name())
	if err != nil {
		t.Fatalf("open pdf: %v", err)
	}
	defer handle.Close()
	extracted, err := reader.GetPlainText()
	if err != nil {
		t.Fatalf("extract: %v", err)
	}
	body, err := io.ReadAll(extracted)
	if err != nil {
		t.Fatalf("read text: %v", err)
	}
	text := string(body)

	for _, fragment := range []string{"Relatorio de atendimento", "1538", "Concluidas"} {
		if !strings.Contains(text, fragment) {
			t.Fatalf("the PDF has no selectable %q; it was rasterised.\nextracted: %q", fragment, text)
		}
	}
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

const widePage = `<!doctype html>
<html><head><meta charset="utf-8"><style>
  @page { size: A4 portrait; margin: 12mm; }
  body { margin: 0; font-family: Arial, sans-serif; }
  .card { border: 1px solid #ccc; padding: 8px; }
</style></head>
<body>
  <div class="card">
    <span id="probe">width marker</span>
  </div>
  <script>
    window.__MEASURED_WIDTH__ = document.body.getBoundingClientRect().width;
    window.__REPORT_READY__ = true;
  </script>
</body></html>`

func TestPrintPageLaysOutAtPaperWidth(t *testing.T) {
	if locateChromium() == "" {
		t.Skip("no chromium executable on this machine")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = io.WriteString(w, widePage)
	}))
	defer server.Close()

	measured, err := measureBodyWidth(server.URL)
	if err != nil {
		t.Fatalf("measure: %v", err)
	}

	if measured > a4WidthPx {
		t.Fatalf("body laid out at %.0fpx, wider than the %dpx page; charts will be clipped",
			measured, a4WidthPx)
	}
	if measured < a4WidthPx/2 {
		t.Fatalf("body laid out at only %.0fpx, far narrower than the page", measured)
	}
}
