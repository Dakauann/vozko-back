package emailhtml

import (
	"errors"
	"strings"
	"testing"

	em "vozko/domain/emailinbox"
)

func TestTheSanitizerStripsEverythingThatRunsOrSubmits(t *testing.T) {
	cases := map[string]struct {
		in      string
		absent  []string
		present []string
	}{
		"script":             {`<p>oi</p><script>alert(1)</script>`, []string{"<script", "alert"}, []string{"<p>oi</p>"}},
		"event handler":      {`<img src="https://a.test/x.png" onerror="alert(1)"><p onclick="x()">oi</p>`, []string{"onerror", "onclick"}, []string{"oi"}},
		"form":               {`<form action="https://evil.test"><input name="senha"><button>ok</button></form><p>oi</p>`, []string{"<form", "<input", "<button"}, []string{"oi"}},
		"iframe":             {`<iframe src="https://evil.test"></iframe><p>oi</p>`, []string{"<iframe"}, []string{"oi"}},
		"javascript url":     {`<a href="javascript:alert(1)">clique</a>`, []string{"javascript:"}, []string{"clique"}},
		"meta refresh":       {`<meta http-equiv="refresh" content="0;url=https://evil.test"><p>oi</p>`, []string{"<meta", "refresh"}, []string{"oi"}},
		"style block":        {`<style>body{background:url(https://t.test/p.gif)}</style><p>oi</p>`, []string{"<style", "t.test"}, []string{"oi"}},
		"object and embed":   {`<object data="x.swf"></object><embed src="x.swf"><p>oi</p>`, []string{"<object", "<embed"}, []string{"oi"}},
		"background url":     {`<table><tr><td style="background-image:url(https://t.test/p.gif);color:#ff0000">oi</td></tr></table>`, []string{"t.test"}, []string{"color"}},
		"inline styles kept": {`<table width="600" style="color: #333333; font-size: 14px"><tr><td align="center">oi</td></tr></table>`, nil, []string{"<table", "font-size", `align="center"`}},
	}
	s := New()
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := s.Sanitize(c.in, true)
			if err != nil {
				t.Fatal(err)
			}
			for _, bad := range c.absent {
				if strings.Contains(strings.ToLower(got.HTML), strings.ToLower(bad)) {
					t.Fatalf("%q survived in %q", bad, got.HTML)
				}
			}
			for _, good := range c.present {
				if !strings.Contains(got.HTML, good) {
					t.Fatalf("%q was lost in %q", good, got.HTML)
				}
			}
		})
	}
}

func TestLinksOpenInANewTabWithoutTheOpenerOrTheReferrer(t *testing.T) {
	got, err := New().Sanitize(`<a href="https://loja.test/p">ver</a>`, true)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`target="_blank"`, "noopener", "noreferrer"} {
		if !strings.Contains(got.HTML, want) {
			t.Fatalf("%s missing in %q", want, got.HTML)
		}
	}
}

func TestRemoteImagesAreBlockedUnlessAllowedAndSaySo(t *testing.T) {
	in := `<p>oi</p><img src="https://tracker.test/open.gif" width="1" height="1"><img src="data:image/png;base64,iVBORw0KGgo=">`
	blocked, err := New().Sanitize(in, false)
	if err != nil {
		t.Fatal(err)
	}
	if !blocked.RemoteImagesBlocked || strings.Contains(blocked.HTML, "tracker.test") || !strings.Contains(blocked.HTML, "data:image/png") {
		t.Fatalf("blocked = %+v", blocked)
	}
	shown, err := New().Sanitize(in, true)
	if err != nil {
		t.Fatal(err)
	}
	if shown.RemoteImagesBlocked || !strings.Contains(shown.HTML, "https://tracker.test/open.gif") {
		t.Fatalf("shown = %+v", shown)
	}
	plain, err := New().Sanitize(`<p>sem imagens</p>`, false)
	if err != nil || plain.RemoteImagesBlocked {
		t.Fatalf("an email without remote images reports nothing blocked: %+v %v", plain, err)
	}
}

func TestTheSanitizerFailsClosedOnWhatItCannotMakeSafe(t *testing.T) {
	for name, in := range map[string]string{
		"empty":         "  ",
		"only a script": "<script>alert(1)</script>",
		"too large":     "<p>" + strings.Repeat("a", em.MaxStoredHTMLBytes) + "</p>",
	} {
		if _, err := New().Sanitize(in, true); !errors.Is(err, em.ErrHTMLUnsafe) {
			t.Fatalf("%s: err = %v, want ErrHTMLUnsafe", name, err)
		}
	}
}
