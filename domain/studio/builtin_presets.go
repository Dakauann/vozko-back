package studio

import (
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

const BuiltinPresetPrefix = "builtin."

var builtinPresetSlug = regexp.MustCompile(`^[a-z][a-zA-Z0-9]{0,63}$`)

var errBuiltinPresetID = errors.New("studio: a built-in preset id needs the builtin prefix and must be unique")

var BuiltinPresetLocales = []string{"pt", "en", "es", "de"}

//go:embed builtin_presets.json
var builtinPresetsJSON []byte

type builtinPresetFile struct {
	Presets []builtinPresetEntry `json:"presets"`
}

type builtinPresetEntry struct {
	ID           string            `json:"id"`
	Name         string            `json:"name"`
	Description  string            `json:"description"`
	Names        map[string]string `json:"names"`
	Descriptions map[string]string `json:"descriptions"`
	Animation    json.RawMessage   `json:"animation"`
}

var loadBuiltinPresets = sync.OnceValues(func() ([]AnimationPreset, error) {
	return parseBuiltinPresets(builtinPresetsJSON)
})

func BuiltinPresets() ([]AnimationPreset, error) {
	presets, err := loadBuiltinPresets()
	if err != nil {
		return nil, err
	}
	return append([]AnimationPreset(nil), presets...), nil
}

func IsBuiltinPresetID(id string) bool {
	return strings.HasPrefix(id, BuiltinPresetPrefix)
}

func BuiltinPreset(id string) (*AnimationPreset, error) {
	presets, err := loadBuiltinPresets()
	if err != nil {
		return nil, err
	}
	for _, p := range presets {
		if p.ID == id {
			found := p
			return &found, nil
		}
	}
	return nil, ErrPresetNotFound
}

func parseBuiltinPresets(raw []byte) ([]AnimationPreset, error) {
	var file builtinPresetFile
	if err := decodeStrict(raw, &file); err != nil {
		return nil, fmt.Errorf("studio: built-in presets: %w", err)
	}
	seen := make(map[string]bool, len(file.Presets))
	presets := make([]AnimationPreset, 0, len(file.Presets))
	for _, entry := range file.Presets {
		if err := entry.issue(seen); err != nil {
			return nil, fmt.Errorf("studio: built-in preset %q: %w", entry.ID, err)
		}
		seen[entry.ID] = true
		presets = append(presets, AnimationPreset{
			ID: entry.ID, Name: entry.Name, Description: entry.Description, Animation: entry.Animation,
			BuiltIn: true, Names: entry.Names, Descriptions: entry.Descriptions,
		})
	}
	return presets, nil
}

func (e builtinPresetEntry) issue(seen map[string]bool) error {
	if !IsBuiltinPresetID(e.ID) || !builtinPresetSlug.MatchString(strings.TrimPrefix(e.ID, BuiltinPresetPrefix)) || seen[e.ID] {
		return errBuiltinPresetID
	}
	for _, locale := range BuiltinPresetLocales {
		if _, err := cleanLabel(e.Names[locale], MaxPresetNameRunes); err != nil {
			return err
		}
		if strings.TrimSpace(e.Descriptions[locale]) == "" {
			return issue(FieldDescription, CodeRequired)
		}
		if _, err := cleanPresetDescription(e.Descriptions[locale]); err != nil {
			return err
		}
	}
	if e.Name != e.Names["pt"] || e.Description != e.Descriptions["pt"] {
		return issue(FieldName, CodeInvalid)
	}
	return ValidatePresetAnimation(e.Animation)
}
