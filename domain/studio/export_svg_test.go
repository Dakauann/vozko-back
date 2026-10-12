package studio

import (
	"errors"
	"os"
	"strings"
	"testing"
)

const svgOpen = `<svg xmlns="http://www.w3.org/2000/svg" xmlns:xlink="http://www.w3.org/1999/xlink" width="100" height="50" viewBox="0 0 100 50">`

func svgWith(body string) []byte {
	return []byte(svgOpen + body + `</svg>`)
}

func TestTheEditorSvgExportIsAccepted(t *testing.T) {
	raw, err := os.ReadFile("testdata/editor_export.svg")
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateExportSVG(raw); err != nil {
		t.Fatalf("the editor output was refused: %v", err)
	}
}

func TestAnSvgExportUsesOnlyWhatTheEditorWrites(t *testing.T) {
	valid := svgWith(`<defs><linearGradient id="g0" gradientUnits="userSpaceOnUse" x1="0" y1="0" x2="100" y2="0"><stop offset="0" stop-color="#ff0000" stop-opacity="0.5"/><stop offset="1" stop-color="#0000ff"/></linearGradient><radialGradient id="g1" gradientUnits="userSpaceOnUse" cx="50" cy="25" r="10"><stop offset="0" stop-color="#ffffff"/></radialGradient></defs>` +
		`<rect x="0" y="0" width="100" height="50" fill="url(#g0)"/>` +
		`<path d="M0 0 L10 0 C10 5 5 10 0 10 Z" fill="#00ff00" fill-opacity="0.5" fill-rule="evenodd" stroke="#111111" stroke-width="2" stroke-linecap="round" stroke-linejoin="miter" stroke-miterlimit="4" stroke-dasharray="4 2" stroke-dashoffset="1" opacity="0.5" style="mix-blend-mode:multiply"/>` +
		`<image x="1.5" y="2" width="20" height="10" preserveAspectRatio="none" xlink:href="data:image/png;base64,iVBORw0KGgo=" style="mix-blend-mode:color-dodge"/>`)
	if err := ValidateExportSVG(valid); err != nil {
		t.Fatalf("valid export refused: %v", err)
	}
}

func TestAnSvgExportWithAnythingElseIsRefused(t *testing.T) {
	cases := map[string][]byte{
		"script":           svgWith(`<script>alert(1)</script>`),
		"event handler":    svgWith(`<rect width="1" height="1" onload="alert(1)"/>`),
		"foreign object":   svgWith(`<foreignObject><div xmlns="http://www.w3.org/1999/xhtml">x</div></foreignObject>`),
		"external image":   svgWith(`<image width="1" height="1" xlink:href="https://evil.test/x.png"/>`),
		"script href":      svgWith(`<image width="1" height="1" xlink:href="javascript:alert(1)"/>`),
		"svg in data":      svgWith(`<image width="1" height="1" xlink:href="data:image/svg+xml;base64,PHN2Zz4="/>`),
		"plain href":       svgWith(`<image width="1" height="1" href="data:image/png;base64,iVBORw0KGgo="/>`),
		"other style":      svgWith(`<rect width="1" height="1" style="fill:url(https://evil.test)"/>`),
		"unknown blend":    svgWith(`<rect width="1" height="1" style="mix-blend-mode:plus-lighter"/>`),
		"external paint":   svgWith(`<rect width="1" height="1" fill="url(https://evil.test/#a)"/>`),
		"named colour":     svgWith(`<rect width="1" height="1" fill="red"/>`),
		"unknown attr":     svgWith(`<rect width="1" height="1" class="x"/>`),
		"text":             svgWith(`<text>hello</text>`),
		"char data":        svgWith(`hello`),
		"comment":          svgWith(`<!-- hi -->`),
		"cdata":            svgWith(`<![CDATA[x]]>`),
		"stop outside":     svgWith(`<stop offset="0" stop-color="#ffffff"/>`),
		"nested svg":       svgWith(`<svg xmlns="http://www.w3.org/2000/svg"></svg>`),
		"use":              svgWith(`<use xlink:href="#a"/>`),
		"path letters":     svgWith(`<path d="M0 0 A1 1 0 0 1 2 2"/>`),
		"bad number":       svgWith(`<rect width="1px" height="1"/>`),
		"other namespace":  []byte(`<svg xmlns="http://example.test/svg" width="1" height="1"></svg>`),
		"prolog":           []byte(`<?xml version="1.0"?>` + svgOpen + `</svg>`),
		"doctype":          []byte(`<!DOCTYPE svg [<!ENTITY x "y">]>` + svgOpen + `</svg>`),
		"entity":           svgWith(`<rect width="&x;" height="1"/>`),
		"unclosed":         []byte(svgOpen + `<rect width="1" height="1">`),
		"trailing element": []byte(svgOpen + `</svg><svg xmlns="http://www.w3.org/2000/svg"></svg>`),
		"root not svg":     []byte(`<rect xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>`),
		"empty":            nil,
	}
	for name, body := range cases {
		if err := ValidateExportSVG(body); !errors.Is(err, ErrInvalidExport) {
			t.Fatalf("%s: err %v", name, err)
		}
	}
}

func TestAnSvgExportAllowsOnlyWhitespaceBetweenElements(t *testing.T) {
	spaced := []byte(strings.Join([]string{svgOpen, `<rect width="1" height="1"/>`, `</svg>`}, "\n  "))
	if err := ValidateExportSVG(spaced); err != nil {
		t.Fatalf("whitespace refused: %v", err)
	}
}
