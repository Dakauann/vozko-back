package mediaprobe

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"image"
	"io"

	ads "vozko/domain/advertising"
)

var ErrUnreadableMedia = errors.New("mediaprobe: the file could not be read")

const (
	boxHeaderBytes        = 8
	largeSizeBytes        = 8
	brandBytes            = 4
	fullBoxHeaderBytes    = 4
	fixedPointShift       = 16
	quickTimeBrand        = "qt  "
	containerWebM         = "webm"
	containerUnknown      = "unknown"
	mvhdV0TimescaleAt     = 12
	mvhdV1TimescaleAt     = 20
	tkhdV0MatrixAt        = 40
	tkhdV1MatrixAt        = 52
	tkhdMatrixBytes       = 36
	tkhdMatrixDAt         = 16
	tkhdSizeBytes         = 8
	millisecondsPerSecond = 1000
	imageHeadBytes        = 64 << 10
	maxMovieBoxBytes      = 64 << 20
)

var ebmlMagic = []byte{0x1A, 0x45, 0xDF, 0xA3}

type Creative struct{}

func NewCreative() Creative { return Creative{} }

func (Creative) Facts(kind ads.MediaKind, file ads.MediaFile) (ads.MediaFacts, error) {
	if kind == ads.MediaVideo {
		return videoFacts(file)
	}
	config, format, err := image.DecodeConfig(bufio.NewReaderSize(io.NewSectionReader(file, 0, file.Size()), imageHeadBytes))
	if err != nil {
		return ads.MediaFacts{}, ErrUnreadableMedia
	}
	return ads.MediaFacts{Kind: kind, Container: format, Bytes: file.Size(), Width: config.Width, Height: config.Height}, nil
}

func videoFacts(file ads.MediaFile) (ads.MediaFacts, error) {
	facts := ads.MediaFacts{Kind: ads.MediaVideo, Bytes: file.Size()}
	head := readHead(file, boxHeaderBytes+brandBytes)
	brand, isISO := isoBrand(head)
	if !isISO {
		facts.Container = containerWebM
		if !bytes.HasPrefix(head, ebmlMagic) {
			facts.Container = containerUnknown
		}
		return facts, nil
	}
	facts.Container = ads.ContainerMP4
	if brand == quickTimeBrand {
		facts.Container = ads.ContainerMOV
	}
	moov, ok := movieBox(file)
	if !ok {
		return ads.MediaFacts{}, ErrUnreadableMedia
	}
	duration, ok := movieDuration(moov)
	if !ok {
		return ads.MediaFacts{}, ErrUnreadableMedia
	}
	width, height, ok := pictureSize(moov)
	if !ok {
		return ads.MediaFacts{}, ErrUnreadableMedia
	}
	facts.DurationMS, facts.Width, facts.Height = duration, width, height
	return facts, nil
}

func readHead(file ads.MediaFile, n int64) []byte {
	head := make([]byte, min(n, file.Size()))
	read, _ := file.ReadAt(head, 0)
	return head[:read]
}

func movieBox(file ads.MediaFile) ([]byte, bool) {
	size := file.Size()
	for at := int64(0); at < size; {
		header := make([]byte, boxHeaderBytes+largeSizeBytes)
		read, _ := file.ReadAt(header, at)
		if read < boxHeaderBytes {
			return nil, false
		}
		length := int64(binary.BigEndian.Uint32(header))
		kind := string(header[4:boxHeaderBytes])
		skip := int64(boxHeaderBytes)
		switch length {
		case 0:
			length = size - at
		case 1:
			if read < boxHeaderBytes+largeSizeBytes {
				return nil, false
			}
			length = int64(binary.BigEndian.Uint64(header[boxHeaderBytes:]))
			skip += largeSizeBytes
		}
		if length < skip || length > size-at {
			return nil, false
		}
		if kind == "moov" {
			if length-skip > maxMovieBoxBytes {
				return nil, false
			}
			body := make([]byte, length-skip)
			if read, _ := file.ReadAt(body, at+skip); int64(read) != length-skip {
				return nil, false
			}
			return body, true
		}
		at += length
	}
	return nil, false
}

func isoBrand(data []byte) (string, bool) {
	if len(data) < boxHeaderBytes+brandBytes || string(data[4:boxHeaderBytes]) != "ftyp" {
		return "", false
	}
	return string(data[boxHeaderBytes : boxHeaderBytes+brandBytes]), true
}

type isoBox struct {
	kind string
	body []byte
}

func boxes(data []byte) ([]isoBox, bool) {
	var out []isoBox
	for len(data) > 0 {
		if len(data) < boxHeaderBytes {
			return nil, false
		}
		size := uint64(binary.BigEndian.Uint32(data))
		kind := string(data[4:8])
		header := uint64(boxHeaderBytes)
		switch size {
		case 0:
			size = uint64(len(data))
		case 1:
			if len(data) < boxHeaderBytes+largeSizeBytes {
				return nil, false
			}
			size = binary.BigEndian.Uint64(data[boxHeaderBytes:])
			header += largeSizeBytes
		}
		if size < header || size > uint64(len(data)) {
			return nil, false
		}
		out = append(out, isoBox{kind: kind, body: data[header:size]})
		data = data[size:]
	}
	return out, true
}

func child(data []byte, kind string) ([]byte, bool) {
	found, ok := boxes(data)
	if !ok {
		return nil, false
	}
	for _, b := range found {
		if b.kind == kind {
			return b.body, true
		}
	}
	return nil, false
}

func movieDuration(moov []byte) (int64, bool) {
	mvhd, ok := child(moov, "mvhd")
	if !ok || len(mvhd) < fullBoxHeaderBytes {
		return 0, false
	}
	var timescale, duration uint64
	if mvhd[0] == 1 {
		if len(mvhd) < mvhdV1TimescaleAt+12 {
			return 0, false
		}
		timescale = uint64(binary.BigEndian.Uint32(mvhd[mvhdV1TimescaleAt:]))
		duration = binary.BigEndian.Uint64(mvhd[mvhdV1TimescaleAt+4:])
	} else {
		if len(mvhd) < mvhdV0TimescaleAt+8 {
			return 0, false
		}
		timescale = uint64(binary.BigEndian.Uint32(mvhd[mvhdV0TimescaleAt:]))
		duration = uint64(binary.BigEndian.Uint32(mvhd[mvhdV0TimescaleAt+4:]))
	}
	if timescale == 0 {
		return 0, false
	}
	return int64(duration * millisecondsPerSecond / timescale), true
}

func pictureSize(moov []byte) (int, int, bool) {
	found, ok := boxes(moov)
	if !ok {
		return 0, 0, false
	}
	for _, b := range found {
		if b.kind != "trak" {
			continue
		}
		tkhd, ok := child(b.body, "tkhd")
		if !ok {
			continue
		}
		if width, height, ok := trackSize(tkhd); ok {
			return width, height, true
		}
	}
	return 0, 0, false
}

func trackSize(tkhd []byte) (int, int, bool) {
	if len(tkhd) < fullBoxHeaderBytes {
		return 0, 0, false
	}
	matrixAt := tkhdV0MatrixAt
	if tkhd[0] == 1 {
		matrixAt = tkhdV1MatrixAt
	}
	sizeAt := matrixAt + tkhdMatrixBytes
	if len(tkhd) < sizeAt+tkhdSizeBytes {
		return 0, 0, false
	}
	width := int(binary.BigEndian.Uint32(tkhd[sizeAt:]) >> fixedPointShift)
	height := int(binary.BigEndian.Uint32(tkhd[sizeAt+4:]) >> fixedPointShift)
	if width <= 0 || height <= 0 {
		return 0, 0, false
	}
	a := binary.BigEndian.Uint32(tkhd[matrixAt:])
	d := binary.BigEndian.Uint32(tkhd[matrixAt+tkhdMatrixDAt:])
	if a == 0 && d == 0 {
		width, height = height, width
	}
	return width, height, true
}
