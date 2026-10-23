package shared

func BasisPoints(n, d int64) int {
	if d <= 0 || n <= 0 {
		return 0
	}
	return int((n*20000 + d) / (2 * d))
}
