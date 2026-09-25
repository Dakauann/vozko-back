package rag

import (
	"encoding/base64"
	"strings"
)

const (
	MetadataEncoding = "encoding"
	EncodingBase64   = "base64"
)

func ContentSize(content string, metadata map[string]string) int64 {
	if metadata[MetadataEncoding] == EncodingBase64 {
		return int64(base64.RawStdEncoding.DecodedLen(len(strings.TrimRight(content, "="))))
	}
	return int64(len(content))
}

func SizeInMB(bytes int64) float64 {
	return float64(bytes) / (1024 * 1024)
}
