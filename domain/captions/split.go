package captions

import "math"

type span struct {
	from, to int
}

func cheapestSplit(n int, cost func(from, to int) (float64, bool)) []span {
	best := make([]float64, n+1)
	cut := make([]int, n+1)
	for to := 1; to <= n; to++ {
		best[to] = math.Inf(1)
		for from := to - 1; from >= 0; from-- {
			c, ok := cost(from, to)
			if !ok {
				break
			}
			if total := best[from] + c; total < best[to] {
				best[to], cut[to] = total, from
			}
		}
	}
	var spans []span
	for to := n; to > 0; to = cut[to] {
		spans = append([]span{{from: cut[to], to: to}}, spans...)
	}
	return spans
}
