package studio

import (
	"bufio"
	"bytes"
	"errors"
	"math"
	"strconv"
	"strings"
)

const (
	MinLutSize   = 2
	MaxLutSize   = 65
	CubeFileExt  = ".cube"
	maxCubeBytes = 16 << 20
)

var (
	ErrCubeEmpty          = errors.New("the LUT file has no table")
	ErrCubeSize           = errors.New("the LUT needs LUT_3D_SIZE from 2 to 65")
	ErrCubeOneDimensional = errors.New("only 3D LUTs are supported")
	ErrCubeRows           = errors.New("the LUT table does not have size cubed rows")
	ErrCubeNumber         = errors.New("a LUT row is not three finite numbers")
	ErrCubeDomain         = errors.New("the LUT domain is invalid")
	ErrCubeKeyword        = errors.New("the LUT file has an unknown keyword")
	ErrCubeTooLarge       = errors.New("the LUT file is too large")
)

type CubeInfo struct {
	Size  int
	Title string
}

func cubeFields(line string, count int) ([]float64, bool) {
	parts := strings.Fields(line)
	if len(parts) != count {
		return nil, false
	}
	values := make([]float64, count)
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		if err != nil || math.IsNaN(v) || math.IsInf(v, 0) {
			return nil, false
		}
		values[i] = v
	}
	return values, true
}

func startsKeyword(line string) bool {
	c := line[0]
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || c == '_'
}

func ParseCube(raw []byte) (CubeInfo, error) {
	if len(raw) > maxCubeBytes {
		return CubeInfo{}, ErrCubeTooLarge
	}
	info := CubeInfo{}
	low := []float64{0, 0, 0}
	high := []float64{1, 1, 1}
	rows := 0
	scanner := bufio.NewScanner(bytes.NewReader(raw))
	scanner.Buffer(make([]byte, 64*1024), maxCubeBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if at := strings.IndexByte(line, '#'); at >= 0 {
			line = line[:at]
		}
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if startsKeyword(line) {
			if rows > 0 {
				return CubeInfo{}, ErrCubeKeyword
			}
			head := strings.Fields(line)[0]
			rest := strings.TrimSpace(strings.TrimPrefix(line, head))
			switch head {
			case "TITLE":
				info.Title = strings.Trim(rest, "\"")
			case "LUT_1D_SIZE", "LUT_1D_INPUT_RANGE":
				return CubeInfo{}, ErrCubeOneDimensional
			case "LUT_3D_SIZE":
				v, ok := cubeFields(rest, 1)
				if !ok || v[0] != math.Trunc(v[0]) || v[0] < MinLutSize || v[0] > MaxLutSize {
					return CubeInfo{}, ErrCubeSize
				}
				info.Size = int(v[0])
			case "DOMAIN_MIN", "DOMAIN_MAX":
				v, ok := cubeFields(rest, 3)
				if !ok {
					return CubeInfo{}, ErrCubeDomain
				}
				if head == "DOMAIN_MIN" {
					low = v
				} else {
					high = v
				}
			case "LUT_3D_INPUT_RANGE":
				v, ok := cubeFields(rest, 2)
				if !ok {
					return CubeInfo{}, ErrCubeDomain
				}
				low = []float64{v[0], v[0], v[0]}
				high = []float64{v[1], v[1], v[1]}
			default:
				return CubeInfo{}, ErrCubeKeyword
			}
			continue
		}
		if _, ok := cubeFields(line, 3); !ok {
			return CubeInfo{}, ErrCubeNumber
		}
		rows++
	}
	if err := scanner.Err(); err != nil {
		return CubeInfo{}, ErrCubeTooLarge
	}
	switch {
	case info.Size == 0 && rows == 0:
		return CubeInfo{}, ErrCubeEmpty
	case info.Size == 0:
		return CubeInfo{}, ErrCubeSize
	case rows != info.Size*info.Size*info.Size:
		return CubeInfo{}, ErrCubeRows
	}
	for c := range low {
		if !(low[c] < high[c]) {
			return CubeInfo{}, ErrCubeDomain
		}
	}
	return info, nil
}
