package emailmime

import (
	"bytes"
	"fmt"
	"io"
	"mime"
	"net/mail"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding"
	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/encoding/unicode"
)

var charsets = map[string]encoding.Encoding{
	"iso-8859-1":   charmap.ISO8859_1,
	"iso8859-1":    charmap.ISO8859_1,
	"latin1":       charmap.ISO8859_1,
	"latin-1":      charmap.ISO8859_1,
	"l1":           charmap.ISO8859_1,
	"iso-8859-15":  charmap.ISO8859_15,
	"windows-1252": charmap.Windows1252,
	"cp1252":       charmap.Windows1252,
	"x-cp1252":     charmap.Windows1252,
	"utf-16":       unicode.UTF16(unicode.BigEndian, unicode.UseBOM),
	"utf-16le":     unicode.UTF16(unicode.LittleEndian, unicode.IgnoreBOM),
	"utf-16be":     unicode.UTF16(unicode.BigEndian, unicode.IgnoreBOM),
}

func normalizeCharset(name string) string {
	return strings.ToLower(strings.Trim(strings.TrimSpace(name), `"`))
}

func isUTF8(name string) bool {
	switch normalizeCharset(name) {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return true
	}
	return false
}

func charsetReader(name string, input io.Reader) (io.Reader, error) {
	if isUTF8(name) {
		return input, nil
	}
	enc, ok := charsets[normalizeCharset(name)]
	if !ok {
		return nil, fmt.Errorf("emailmime: unsupported charset %q", name)
	}
	return enc.NewDecoder().Reader(input), nil
}

var wordDecoder = &mime.WordDecoder{CharsetReader: charsetReader}

var addressParser = &mail.AddressParser{WordDecoder: wordDecoder}

func decodeHeader(raw string) string {
	decoded, err := wordDecoder.DecodeHeader(raw)
	if err != nil {
		decoded = raw
	}
	return strings.Join(strings.Fields(strings.ToValidUTF8(decoded, "�")), " ")
}

func decodeCharset(name string, data []byte) string {
	if !isUTF8(name) {
		if enc, ok := charsets[normalizeCharset(name)]; ok {
			if decoded, err := enc.NewDecoder().Bytes(data); err == nil {
				return string(decoded)
			}
		}
	}
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	if utf8.Valid(data) {
		return string(data)
	}
	return strings.ToValidUTF8(string(data), "�")
}
