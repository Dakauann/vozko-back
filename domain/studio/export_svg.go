package studio

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"regexp"
	"strings"
)

const (
	svgNamespace   = "http://www.w3.org/2000/svg"
	xlinkNamespace = "http://www.w3.org/1999/xlink"
	xmlnsPrefix    = "xmlns"
)

type svgValue func(string) bool

func pattern(expr string) svgValue {
	re := regexp.MustCompile(expr)
	return re.MatchString
}

func oneOf(values ...string) svgValue {
	return func(v string) bool {
		for _, candidate := range values {
			if v == candidate {
				return true
			}
		}
		return false
	}
}

func either(checks ...svgValue) svgValue {
	return func(v string) bool {
		for _, check := range checks {
			if check(v) {
				return true
			}
		}
		return false
	}
}

var (
	svgNumber  = pattern(`^-?(\d+(\.\d+)?|\.\d+)(e[-+]?\d+)?$`)
	svgNumbers = pattern(`^-?[0-9.eE+\- ,]+$`)
	svgPath    = pattern(`^[MLCZ0-9.eE+\- ,]*$`)
	svgColor   = pattern(`^#[0-9a-f]{6}$`)
	svgID      = pattern(`^[A-Za-z][A-Za-z0-9_-]*$`)
	svgPaintID = pattern(`^url\(#[A-Za-z][A-Za-z0-9_-]*\)$`)
	svgImage   = pattern(`^data:image/(png|jpeg);base64,[A-Za-z0-9+/]+={0,2}$`)
	svgBlend   = pattern(`^mix-blend-mode:(multiply|screen|overlay|darken|lighten|color-dodge|color-burn|hard-light|soft-light|difference|exclusion|hue|saturation|color|luminosity)$`)
	svgPaint   = either(oneOf("none"), svgColor, svgPaintID)
)

var shapePaint = map[string]svgValue{
	"fill":              svgPaint,
	"fill-opacity":      svgNumber,
	"fill-rule":         oneOf("nonzero", "evenodd"),
	"stroke":            svgPaint,
	"stroke-opacity":    svgNumber,
	"stroke-width":      svgNumber,
	"stroke-linecap":    oneOf("butt", "round", "square"),
	"stroke-linejoin":   oneOf("miter", "round", "bevel"),
	"stroke-miterlimit": svgNumber,
	"stroke-dasharray":  svgNumbers,
	"stroke-dashoffset": svgNumber,
	"opacity":           svgNumber,
	"style":             svgBlend,
}

func withPaint(own map[string]svgValue) map[string]svgValue {
	merged := make(map[string]svgValue, len(own)+len(shapePaint))
	for name, check := range shapePaint {
		merged[name] = check
	}
	for name, check := range own {
		merged[name] = check
	}
	return merged
}

type svgElement struct {
	attributes map[string]svgValue
	children   []string
}

var svgElements = map[string]svgElement{
	"svg": {
		attributes: map[string]svgValue{"width": svgNumber, "height": svgNumber, "viewBox": svgNumbers},
		children:   []string{"defs", "rect", "path", "image"},
	},
	"defs": {children: []string{"linearGradient", "radialGradient"}},
	"linearGradient": {
		attributes: map[string]svgValue{"id": svgID, "gradientUnits": oneOf("userSpaceOnUse"), "x1": svgNumber, "y1": svgNumber, "x2": svgNumber, "y2": svgNumber},
		children:   []string{"stop"},
	},
	"radialGradient": {
		attributes: map[string]svgValue{"id": svgID, "gradientUnits": oneOf("userSpaceOnUse"), "cx": svgNumber, "cy": svgNumber, "r": svgNumber},
		children:   []string{"stop"},
	},
	"stop": {attributes: map[string]svgValue{"offset": svgNumber, "stop-color": svgColor, "stop-opacity": svgNumber}},
	"rect": {attributes: withPaint(map[string]svgValue{"x": svgNumber, "y": svgNumber, "width": svgNumber, "height": svgNumber})},
	"path": {attributes: withPaint(map[string]svgValue{"d": svgPath})},
	"image": {
		attributes: map[string]svgValue{"x": svgNumber, "y": svgNumber, "width": svgNumber, "height": svgNumber, "preserveAspectRatio": oneOf("none"), "opacity": svgNumber, "style": svgBlend},
	},
}

var errSvgShape = errors.New("studio: svg export outside the editor vocabulary")

func ValidateExportSVG(raw []byte) error {
	if err := walkExportSVG(raw); err != nil {
		return errors.Join(ErrInvalidExport, err)
	}
	return nil
}

func walkExportSVG(raw []byte) error {
	decoder := xml.NewDecoder(bytes.NewReader(raw))
	decoder.Strict = true
	var open []string
	rootClosed := false
	for {
		token, err := decoder.RawToken()
		if errors.Is(err, io.EOF) {
			if !rootClosed || len(open) > 0 {
				return errSvgShape
			}
			return nil
		}
		if err != nil {
			return err
		}
		switch tok := token.(type) {
		case xml.StartElement:
			if rootClosed || !svgChildAllowed(open, tok) {
				return errSvgShape
			}
			if err := svgAttributesAllowed(tok, len(open) == 0); err != nil {
				return err
			}
			open = append(open, tok.Name.Local)
		case xml.EndElement:
			if len(open) == 0 || open[len(open)-1] != tok.Name.Local || tok.Name.Space != "" {
				return errSvgShape
			}
			open = open[:len(open)-1]
			rootClosed = len(open) == 0
		case xml.CharData:
			if len(bytes.TrimSpace(tok)) > 0 {
				return errSvgShape
			}
		default:
			return errSvgShape
		}
	}
}

func svgChildAllowed(open []string, tok xml.StartElement) bool {
	if tok.Name.Space != "" {
		return false
	}
	if len(open) == 0 {
		return tok.Name.Local == "svg"
	}
	for _, child := range svgElements[open[len(open)-1]].children {
		if child == tok.Name.Local {
			return true
		}
	}
	return false
}

func svgAttributesAllowed(tok xml.StartElement, root bool) error {
	element := svgElements[tok.Name.Local]
	seen := make(map[string]bool, len(tok.Attr))
	namespaced := false
	for _, attr := range tok.Attr {
		key := attr.Name.Space + ":" + attr.Name.Local
		if seen[key] {
			return errSvgShape
		}
		seen[key] = true
		switch {
		case root && attr.Name.Space == "" && attr.Name.Local == xmlnsPrefix:
			if attr.Value != svgNamespace {
				return errSvgShape
			}
			namespaced = true
		case root && attr.Name.Space == xmlnsPrefix && attr.Name.Local == "xlink":
			if attr.Value != xlinkNamespace {
				return errSvgShape
			}
		case attr.Name.Space == "xlink" && attr.Name.Local == "href" && tok.Name.Local == "image":
			if !svgImage(attr.Value) {
				return errSvgShape
			}
		case attr.Name.Space == "":
			check, ok := element.attributes[attr.Name.Local]
			if !ok || !check(strings.TrimSpace(attr.Value)) || attr.Value != strings.TrimSpace(attr.Value) {
				return errSvgShape
			}
		default:
			return errSvgShape
		}
	}
	if root && !namespaced {
		return errSvgShape
	}
	if tok.Name.Local == "image" && !seen["xlink:href"] {
		return errSvgShape
	}
	return nil
}
