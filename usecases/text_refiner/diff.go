package text_refiner

import (
	"fmt"
	"strings"
	"unicode"

	"vozko/domain/text_refiner"
)

func computeSegments(original, refined string) []text_refiner.DiffSegment {
	a := tokenise(original)
	b := tokenise(refined)
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	ops := lcsDiff(a, b)
	return mergeOps(ops)
}

func tokenise(s string) []string {
	if s == "" {
		return nil
	}
	tokens := make([]string, 0, len(s)/4+1)
	runes := []rune(s)
	i := 0
	for i < len(runes) {
		r := runes[i]
		switch {
		case unicode.IsSpace(r):
			j := i + 1
			for j < len(runes) && unicode.IsSpace(runes[j]) {
				j++
			}
			tokens = append(tokens, string(runes[i:j]))
			i = j
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			j := i + 1
			for j < len(runes) && (unicode.IsLetter(runes[j]) || unicode.IsDigit(runes[j])) {
				j++
			}
			tokens = append(tokens, string(runes[i:j]))
			i = j
		default:
			tokens = append(tokens, string(r))
			i++
		}
	}
	return tokens
}

type op struct {
	kind text_refiner.DiffOp
	text string
}

func lcsDiff(a, b []string) []op {
	n, m := len(a), len(b)
	if n == 0 {
		out := make([]op, 0, m)
		for _, t := range b {
			out = append(out, op{text_refiner.DiffOpInsert, t})
		}
		return out
	}
	if m == 0 {
		out := make([]op, 0, n)
		for _, t := range a {
			out = append(out, op{text_refiner.DiffOpDelete, t})
		}
		return out
	}

	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if a[i] == b[j] {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	out := make([]op, 0, n+m)
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case a[i] == b[j]:
			out = append(out, op{text_refiner.DiffOpEqual, a[i]})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			out = append(out, op{text_refiner.DiffOpDelete, a[i]})
			i++
		default:
			out = append(out, op{text_refiner.DiffOpInsert, b[j]})
			j++
		}
	}
	for ; i < n; i++ {
		out = append(out, op{text_refiner.DiffOpDelete, a[i]})
	}
	for ; j < m; j++ {
		out = append(out, op{text_refiner.DiffOpInsert, b[j]})
	}
	return out
}

func mergeOps(ops []op) []text_refiner.DiffSegment {
	if len(ops) == 0 {
		return nil
	}
	out := make([]text_refiner.DiffSegment, 0, len(ops))
	cur := text_refiner.DiffSegment{Op: ops[0].kind, Text: ops[0].text}
	for k := 1; k < len(ops); k++ {
		if ops[k].kind == cur.Op {
			cur.Text += ops[k].text
			continue
		}
		out = append(out, cur)
		cur = text_refiner.DiffSegment{Op: ops[k].kind, Text: ops[k].text}
	}
	out = append(out, cur)
	return out
}

const unifiedContext = 3

type diffRec struct {
	kind  text_refiner.DiffOp
	text  string
	oldLn int
	newLn int
}

func buildUnifiedDiff(original, refined string) string {
	a := splitLinesKeepEnd(original)
	b := splitLinesKeepEnd(refined)
	ops := lcsDiff(a, b)

	if len(ops) == 0 {
		return ""
	}

	recs := make([]diffRec, 0, len(ops))
	oldLn, newLn := 1, 1
	hasChange := false
	for _, o := range ops {
		switch o.kind {
		case text_refiner.DiffOpEqual:
			recs = append(recs, diffRec{o.kind, o.text, oldLn, newLn})
			oldLn++
			newLn++
		case text_refiner.DiffOpDelete:
			recs = append(recs, diffRec{o.kind, o.text, oldLn, 0})
			oldLn++
			hasChange = true
		case text_refiner.DiffOpInsert:
			recs = append(recs, diffRec{o.kind, o.text, 0, newLn})
			newLn++
			hasChange = true
		}
	}
	if !hasChange {
		return ""
	}

	var b2 strings.Builder
	b2.WriteString("--- original\n+++ refined\n")

	i := 0
	for i < len(recs) {
		if recs[i].kind == text_refiner.DiffOpEqual {
			i++
			continue
		}

		start := i
		for start > 0 && recs[start-1].kind == text_refiner.DiffOpEqual && start > i-unifiedContext {
			start--
		}
		end := i
		for end < len(recs) {
			if recs[end].kind != text_refiner.DiffOpEqual {
				end++
				continue
			}

			gap := 0
			j := end
			for j < len(recs) && recs[j].kind == text_refiner.DiffOpEqual {
				gap++
				j++
			}
			if j < len(recs) && gap <= 2*unifiedContext {
				end = j
				continue
			}
			break
		}

		stop := end
		ctx := 0
		for stop < len(recs) && recs[stop].kind == text_refiner.DiffOpEqual && ctx < unifiedContext {
			stop++
			ctx++
		}

		writeHunk(&b2, recs[start:stop])
		i = stop
	}
	return b2.String()
}

func writeHunk(b *strings.Builder, recs []diffRec) {
	if len(recs) == 0 {
		return
	}
	oldStart, newStart := 0, 0
	oldLen, newLen := 0, 0
	for _, r := range recs {
		switch r.kind {
		case text_refiner.DiffOpEqual:
			if oldStart == 0 {
				oldStart = r.oldLn
			}
			if newStart == 0 {
				newStart = r.newLn
			}
			oldLen++
			newLen++
		case text_refiner.DiffOpDelete:
			if oldStart == 0 {
				oldStart = r.oldLn
			}
			oldLen++
		case text_refiner.DiffOpInsert:
			if newStart == 0 {
				newStart = r.newLn
			}
			newLen++
		}
	}
	if oldStart == 0 {
		oldStart = 1
	}
	if newStart == 0 {
		newStart = 1
	}
	fmt.Fprintf(b, "@@ -%d,%d +%d,%d @@\n", oldStart, oldLen, newStart, newLen)
	for _, r := range recs {
		line := stripTrailingNewline(r.text)
		switch r.kind {
		case text_refiner.DiffOpEqual:
			b.WriteString(" ")
		case text_refiner.DiffOpDelete:
			b.WriteString("-")
		case text_refiner.DiffOpInsert:
			b.WriteString("+")
		}
		b.WriteString(line)
		b.WriteString("\n")
	}
}

func splitLinesKeepEnd(s string) []string {
	if s == "" {
		return nil
	}
	out := make([]string, 0, strings.Count(s, "\n")+1)
	for {
		idx := strings.IndexByte(s, '\n')
		if idx < 0 {
			out = append(out, s)
			return out
		}
		out = append(out, s[:idx+1])
		s = s[idx+1:]
		if s == "" {
			return out
		}
	}
}

func stripTrailingNewline(s string) string {
	s = strings.TrimRight(s, "\r")
	if strings.HasSuffix(s, "\n") {
		return s[:len(s)-1]
	}
	return s
}
