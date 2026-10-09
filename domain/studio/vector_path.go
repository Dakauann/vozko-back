package studio

import (
	"math"
	"strconv"
	"strings"
)

const MaxPathCoordinate = 1000

const pathCommands = "MmLlHhVvCcSsQqTtAaZz"

var pathArity = map[byte]int{'m': 2, 'l': 2, 't': 2, 'h': 1, 'v': 1, 'c': 6, 's': 4, 'q': 4, 'a': 7, 'z': 0}

type pathSegment struct {
	command byte
	values  []float64
}

func ValidPath(data string) bool {
	segments, ok := pathSegments(data)
	if !ok || len(segments) == 0 || lowerCommand(segments[0].command) != 'm' {
		return false
	}
	for _, s := range segments {
		if !s.valid() {
			return false
		}
	}
	return true
}

func (s pathSegment) valid() bool {
	command := lowerCommand(s.command)
	arity := pathArity[command]
	if arity == 0 {
		return len(s.values) == 0
	}
	if len(s.values) == 0 || len(s.values)%arity != 0 {
		return false
	}
	for i, v := range s.values {
		if flag := i % arity; command == 'a' && (flag == 3 || flag == 4) && v != 0 && v != 1 {
			return false
		}
	}
	return true
}

func pathSegments(data string) ([]pathSegment, bool) {
	var segments []pathSegment
	i := 0
	for {
		for i < len(data) && isPathSeparator(data[i]) {
			i++
		}
		if i == len(data) {
			return segments, true
		}
		if strings.IndexByte(pathCommands, data[i]) >= 0 {
			segments = append(segments, pathSegment{command: data[i]})
			i++
			continue
		}
		n := pathNumberLength(data[i:])
		if n == 0 || len(segments) == 0 {
			return nil, false
		}
		v, err := strconv.ParseFloat(data[i:i+n], 64)
		if err != nil || math.Abs(v) > MaxPathCoordinate {
			return nil, false
		}
		last := &segments[len(segments)-1]
		last.values = append(last.values, v)
		i += n
	}
}

func pathNumberLength(s string) int {
	i := 0
	if i < len(s) && (s[i] == '+' || s[i] == '-') {
		i++
	}
	whole := digitsFrom(s, i)
	switch {
	case whole > i:
		i = whole
		if i < len(s) && s[i] == '.' {
			i = digitsFrom(s, i+1)
		}
	case i < len(s) && s[i] == '.' && digitsFrom(s, i+1) > i+1:
		i = digitsFrom(s, i+1)
	default:
		return 0
	}
	if i < len(s) && (s[i] == 'e' || s[i] == 'E') {
		j := i + 1
		if j < len(s) && (s[j] == '+' || s[j] == '-') {
			j++
		}
		if end := digitsFrom(s, j); end > j {
			i = end
		}
	}
	return i
}

func digitsFrom(s string, i int) int {
	for i < len(s) && s[i] >= '0' && s[i] <= '9' {
		i++
	}
	return i
}

func isPathSeparator(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\r' || c == '\f' || c == '\v' || c == ','
}

func lowerCommand(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + ('a' - 'A')
	}
	return c
}
