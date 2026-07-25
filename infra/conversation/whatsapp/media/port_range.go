package media

func PortRangeOverlaps(aStart, aEnd, bStart, bEnd int) bool {
	if aStart > aEnd {
		aStart, aEnd = aEnd, aStart
	}
	if bStart > bEnd {
		bStart, bEnd = bEnd, bStart
	}
	return aStart <= bEnd && bStart <= aEnd
}
