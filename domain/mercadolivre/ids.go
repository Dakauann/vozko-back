package mercadolivre

import (
	"bytes"
	"encoding/json"
	"errors"
	"regexp"
)

const SiteMLB = "MLB"

const maxNumericIDDigits = 20

var itemIDPattern = regexp.MustCompile(`^MLB[0-9]{1,20}$`)

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

func ValidItemID(id string) bool { return itemIDPattern.MatchString(id) }

func SupportedSite(site string) bool { return site == SiteMLB }

var errFlexibleID = errors.New("mercado livre id must be an integer or a string of digits")

type FlexibleID string

func (f *FlexibleID) UnmarshalJSON(raw []byte) error {
	raw = bytes.TrimSpace(raw)
	if bytes.Equal(raw, []byte("null")) {
		*f = ""
		return nil
	}
	var value string
	if len(raw) > 0 && raw[0] == '"' {
		if err := json.Unmarshal(raw, &value); err != nil {
			return err
		}
		if value == "" {
			*f = ""
			return nil
		}
	} else {
		value = string(raw)
	}
	if !ValidNumericID(value) {
		return errFlexibleID
	}
	*f = FlexibleID(value)
	return nil
}

func (f FlexibleID) String() string { return string(f) }
