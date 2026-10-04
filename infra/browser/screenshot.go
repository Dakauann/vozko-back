package browser

import (
	"context"
	"errors"
	"fmt"

	"github.com/chromedp/cdproto/page"
	"github.com/chromedp/chromedp"
)

const (
	pageSettled  = "window.__PAGE_READY__ === true || window.__PAGE_FAILED__ === true"
	pageFailed   = "window.__PAGE_FAILED__ === true"
	blankAddress = "about:blank"
)

var ErrPageFailed = errors.New("browser: the page reported a failure while loading")

func (r *Renderer) Screenshot(ctx context.Context, html string, width, height int) ([]byte, error) {
	if width <= 0 || height <= 0 {
		return nil, fmt.Errorf("browser: invalid screenshot size %dx%d", width, height)
	}
	sessionCtx, done, err := r.session(ctx, int64(width), int64(height))
	if err != nil {
		return nil, err
	}
	defer done()

	var failed bool
	var shot []byte
	err = chromedp.Run(sessionCtx,
		chromedp.EmulateViewport(int64(width), int64(height)),
		chromedp.Navigate(blankAddress),
		chromedp.ActionFunc(func(ctx context.Context) error {
			tree, err := page.GetFrameTree().Do(ctx)
			if err != nil {
				return err
			}
			return page.SetDocumentContent(tree.Frame.ID, html).Do(ctx)
		}),
		chromedp.Poll(pageSettled, nil, chromedp.WithPollingInterval(readyPoll)),
		chromedp.Evaluate(pageFailed, &failed),
		chromedp.ActionFunc(func(ctx context.Context) error {
			if failed {
				return ErrPageFailed
			}
			data, err := page.CaptureScreenshot().
				WithFormat(page.CaptureScreenshotFormatPng).
				WithClip(&page.Viewport{X: 0, Y: 0, Width: float64(width), Height: float64(height), Scale: 1}).
				Do(ctx)
			shot = data
			return err
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("browser: rendering the screenshot: %w", err)
	}
	if len(shot) == 0 {
		return nil, fmt.Errorf("browser: the screenshot came back empty")
	}
	return shot, nil
}
