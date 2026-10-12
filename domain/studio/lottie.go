package studio

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"path/filepath"
	"strings"
)

const (
	LottieJSONExt    = ".json"
	LottieArchiveExt = ".lottie"
	maxLottieEntry   = 64 << 20
)

var ErrNotLottie = errors.New("the file is not a Lottie animation")

type lottieHead struct {
	FrameRate *float64         `json:"fr"`
	In        *float64         `json:"ip"`
	Out       *float64         `json:"op"`
	Width     *float64         `json:"w"`
	Height    *float64         `json:"h"`
	Layers    *json.RawMessage `json:"layers"`
}

func positive(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0) && *value > 0
}

func finiteNumber(value *float64) bool {
	return value != nil && !math.IsNaN(*value) && !math.IsInf(*value, 0)
}

func lottieAnimation(data []byte) error {
	var head lottieHead
	if err := json.Unmarshal(data, &head); err != nil {
		return fmt.Errorf("%w: %v", ErrNotLottie, err)
	}
	if !positive(head.FrameRate) || !finiteNumber(head.In) || !finiteNumber(head.Out) || *head.Out <= *head.In || !positive(head.Width) || !positive(head.Height) || head.Layers == nil {
		return ErrNotLottie
	}
	var layers []json.RawMessage
	if err := json.Unmarshal(*head.Layers, &layers); err != nil {
		return fmt.Errorf("%w: layers is not a list", ErrNotLottie)
	}
	return nil
}

func zipEntry(archive *zip.Reader, name string) ([]byte, bool, error) {
	for _, file := range archive.File {
		if file.Name != name {
			continue
		}
		if file.UncompressedSize64 > maxLottieEntry {
			return nil, true, fmt.Errorf("%w: %s is too large", ErrNotLottie, name)
		}
		reader, err := file.Open()
		if err != nil {
			return nil, true, fmt.Errorf("%w: %v", ErrNotLottie, err)
		}
		defer reader.Close()
		data, err := io.ReadAll(io.LimitReader(reader, maxLottieEntry+1))
		if err != nil {
			return nil, true, fmt.Errorf("%w: %v", ErrNotLottie, err)
		}
		return data, true, nil
	}
	return nil, false, nil
}

func dotLottieAnimation(data []byte) error {
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrNotLottie, err)
	}
	manifest, found, err := zipEntry(archive, "manifest.json")
	if err != nil {
		return err
	}
	if !found {
		return fmt.Errorf("%w: the archive has no manifest", ErrNotLottie)
	}
	var parsed struct {
		Animations []struct {
			ID string `json:"id"`
		} `json:"animations"`
	}
	if err := json.Unmarshal(manifest, &parsed); err != nil || len(parsed.Animations) == 0 || parsed.Animations[0].ID == "" {
		return fmt.Errorf("%w: the manifest names no animation", ErrNotLottie)
	}
	for _, folder := range []string{"animations", "a"} {
		animation, found, err := zipEntry(archive, folder+"/"+parsed.Animations[0].ID+LottieJSONExt)
		if err != nil {
			return err
		}
		if found {
			return lottieAnimation(animation)
		}
	}
	return fmt.Errorf("%w: the first animation is missing", ErrNotLottie)
}

func ParseLottie(name string, data []byte) error {
	switch strings.ToLower(filepath.Ext(name)) {
	case LottieJSONExt:
		return lottieAnimation(data)
	case LottieArchiveExt:
		return dotLottieAnimation(data)
	}
	return fmt.Errorf("%w: use a %s or %s file", ErrNotLottie, LottieJSONExt, LottieArchiveExt)
}
