package browser

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const (
	readyFlag      = "window.__REPORT_READY__ === true"
	readyPoll      = 150 * time.Millisecond
	defaultTimeout = 90 * time.Second

	a4WidthPx  = 794
	a4HeightPx = 1123
)

type PDFOptions struct {
	Landscape bool

	ViewportWidth  int64
	ViewportHeight int64
}

func A4Portrait() PDFOptions {
	return PDFOptions{ViewportWidth: a4WidthPx, ViewportHeight: a4HeightPx}
}

type Renderer struct {
	timeout time.Duration
}

func NewRenderer() *Renderer {
	return &Renderer{timeout: defaultTimeout}
}

func (r *Renderer) Available() bool {
	return r != nil && locateChromium() != ""
}

func locateChromium() string {
	if custom := strings.TrimSpace(os.Getenv("CHROMIUM_PATH")); custom != "" {
		if _, err := os.Stat(custom); err == nil {
			return custom
		}
	}
	candidates := []string{
		"/usr/bin/chromium",
		"/usr/bin/chromium-browser",
		"/usr/bin/google-chrome",
		"/usr/bin/google-chrome-stable",
		"C:/Program Files (x86)/Microsoft/Edge/Application/msedge.exe",
		"C:/Program Files/Google/Chrome/Application/chrome.exe",
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return candidate
		}
	}
	return ""
}

func (r *Renderer) Render(ctx context.Context, url string, options PDFOptions) ([]byte, error) {
	executable := locateChromium()
	if executable == "" {
		return nil, fmt.Errorf("browser: no chromium executable was found; set CHROMIUM_PATH")
	}

	width, height := options.ViewportWidth, options.ViewportHeight
	if width <= 0 {
		width = a4WidthPx
	}
	if height <= 0 {
		height = a4HeightPx
	}

	allocatorOptions := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.ExecPath(executable),
		chromedp.Flag("headless", "new"),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
		chromedp.Flag("disable-dev-shm-usage", true),
		chromedp.Flag("hide-scrollbars", true),
		chromedp.WindowSize(int(width), int(height)),
	)

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	defer cancelAllocator()

	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	timeoutCtx, cancelTimeout := context.WithTimeout(browserCtx, r.timeout)
	defer cancelTimeout()

	var pdf []byte
	err := chromedp.Run(timeoutCtx,
		chromedp.EmulateViewport(width, height),
		chromedp.Navigate(url),
		chromedp.Poll(readyFlag, nil, chromedp.WithPollingInterval(readyPoll)),
		chromedp.ActionFunc(func(ctx context.Context) error {
			data, _, err := page.PrintToPDF().
				WithPrintBackground(true).
				WithPreferCSSPageSize(true).
				WithLandscape(options.Landscape).
				Do(ctx)
			if err != nil {
				return err
			}
			pdf = data
			return nil
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("browser: rendering the print page: %w", err)
	}
	if len(pdf) == 0 {
		return nil, fmt.Errorf("browser: the print page produced an empty document")
	}
	return pdf, nil
}

func measureBodyWidth(url string) (float64, error) {
	executable := locateChromium()
	if executable == "" {
		return 0, fmt.Errorf("browser: no chromium executable was found")
	}

	allocatorCtx, cancelAllocator := chromedp.NewExecAllocator(context.Background(),
		append(chromedp.DefaultExecAllocatorOptions[:],
			chromedp.ExecPath(executable),
			chromedp.Flag("headless", "new"),
			chromedp.Flag("no-sandbox", true),
			chromedp.WindowSize(a4WidthPx, a4HeightPx),
		)...)
	defer cancelAllocator()

	browserCtx, cancelBrowser := chromedp.NewContext(allocatorCtx)
	defer cancelBrowser()

	timeoutCtx, cancelTimeout := context.WithTimeout(browserCtx, defaultTimeout)
	defer cancelTimeout()

	var width float64
	err := chromedp.Run(timeoutCtx,
		chromedp.EmulateViewport(a4WidthPx, a4HeightPx),
		chromedp.Navigate(url),
		chromedp.Poll(readyFlag, nil, chromedp.WithPollingInterval(readyPoll)),
		chromedp.Evaluate("window.__MEASURED_WIDTH__", &width),
	)
	return width, err
}
