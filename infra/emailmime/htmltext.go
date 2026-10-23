package emailmime

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var (
	skippedElements = map[atom.Atom]bool{
		atom.Script: true, atom.Style: true, atom.Head: true, atom.Title: true,
		atom.Noscript: true, atom.Template: true, atom.Svg: true, atom.Object: true,
		atom.Iframe: true, atom.Select: true,
	}
	paragraphElements = map[atom.Atom]bool{
		atom.P: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
		atom.Blockquote: true, atom.Table: true, atom.Ul: true, atom.Ol: true, atom.Pre: true, atom.Hr: true,
	}
	lineElements = map[atom.Atom]bool{
		atom.Br: true, atom.Div: true, atom.Li: true, atom.Tr: true, atom.Section: true, atom.Article: true,
		atom.Header: true, atom.Footer: true, atom.Dt: true, atom.Dd: true,
	}
	whitespaceRun = regexp.MustCompile(`[\s\x{00a0}]+`)
	blankLines    = regexp.MustCompile(`\n{3,}`)
)

type textWriter struct {
	b strings.Builder
}

func (w *textWriter) text(s string) {
	s = whitespaceRun.ReplaceAllString(s, " ")
	if s == "" {
		return
	}
	current := w.b.String()
	if current == "" || strings.HasSuffix(current, "\n") || strings.HasSuffix(current, " ") {
		s = strings.TrimLeft(s, " ")
	}
	w.b.WriteString(s)
}

func (w *textWriter) breakLines(n int) {
	current := strings.TrimRight(w.b.String(), " ")
	if current == "" {
		w.b.Reset()
		return
	}
	have := len(current) - len(strings.TrimRight(current, "\n"))
	w.b.Reset()
	w.b.WriteString(current)
	for ; have < n; have++ {
		w.b.WriteByte('\n')
	}
}

func HTMLToText(markup string) string {
	tokenizer := html.NewTokenizer(strings.NewReader(markup))
	var w textWriter
	skipping := 0
	var links []string
	var linkText []int
	for {
		switch tokenizer.Next() {
		case html.ErrorToken:
			return finish(w.b.String())
		case html.TextToken:
			if skipping == 0 {
				w.text(string(tokenizer.Text()))
			}
		case html.StartTagToken, html.SelfClosingTagToken:
			token := tokenizer.Token()
			if skippedElements[token.DataAtom] {
				if token.Type == html.StartTagToken {
					skipping++
				}
				continue
			}
			if skipping > 0 {
				continue
			}
			switch {
			case token.DataAtom == atom.A && token.Type == html.StartTagToken:
				links = append(links, linkTarget(token))
				linkText = append(linkText, w.b.Len())
			case paragraphElements[token.DataAtom]:
				w.breakLines(2)
			case lineElements[token.DataAtom]:
				w.breakLines(1)
			}
		case html.EndTagToken:
			token := tokenizer.Token()
			if skippedElements[token.DataAtom] {
				if skipping > 0 {
					skipping--
				}
				continue
			}
			if skipping > 0 {
				continue
			}
			switch {
			case token.DataAtom == atom.A && len(links) > 0:
				target, start := links[len(links)-1], linkText[len(linkText)-1]
				links, linkText = links[:len(links)-1], linkText[:len(linkText)-1]
				label := strings.TrimSpace(w.b.String()[min(start, w.b.Len()):])
				if target != "" && label != "" && label != target && strings.TrimPrefix(target, "mailto:") != label {
					w.text(" (" + target + ")")
				}
			case paragraphElements[token.DataAtom]:
				w.breakLines(2)
			case lineElements[token.DataAtom]:
				w.breakLines(1)
			}
		}
	}
}

func linkTarget(token html.Token) string {
	for _, attr := range token.Attr {
		if attr.Key != "href" {
			continue
		}
		href := strings.TrimSpace(attr.Val)
		lower := strings.ToLower(href)
		if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "mailto:") {
			return href
		}
	}
	return ""
}

func finish(text string) string {
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.TrimSpace(blankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}
