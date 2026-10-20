package tiktok

import (
	"bytes"
	"encoding/json"
	"errors"
)

const maxNumericIDDigits = 20

const MaxOpaqueIDLength = 256

func ValidNumericID(id string) bool {
	if id == "" || len(id) > maxNumericIDDigits {
		return false
	}
	for i := 0; i < len(id); i++ {
		if id[i] < '0' || id[i] > '9' {
			return false
		}
	}
	return true
}

func ValidOpaqueID(id string) bool {
	if id == "" || len(id) > MaxOpaqueIDLength {
		return false
	}
	for i := 0; i < len(id); i++ {
		if !opaqueIDByte(id[i]) {
			return false
		}
	}
	return true
}

func opaqueIDByte(c byte) bool {
	switch {
	case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		return true
	}
	switch c {
	case '+', '/', '=', '_', '-', ':', '.':
		return true
	}
	return false
}

var errNumericID = errors.New("tiktok id must be an integer or a string of digits")

type NumericID string

func (n *NumericID) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		*n = ""
		return nil
	}
	value := string(raw)
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if value == "" {
			*n = ""
			return nil
		}
	}
	if !ValidNumericID(value) {
		return errNumericID
	}
	*n = NumericID(value)
	return nil
}

func (n NumericID) String() string { return string(n) }
