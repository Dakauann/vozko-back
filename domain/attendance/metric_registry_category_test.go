package attendance

import "testing"

func TestEveryTargetableMetricHasACategory(t *testing.T) {
	for _, spec := range TargetableMetrics() {
		if spec.Category == "" {
			t.Errorf("metric %q has no category; the goals dialog would drop it into an unnamed group", spec.Key)
		}
		if spec.Category.Order() >= 4 {
			t.Errorf("metric %q has the unknown category %q", spec.Key, spec.Category)
		}
	}
}

func TestTargetableMetricsComeBackGroupedByCategory(t *testing.T) {
	metrics := TargetableMetrics()

	seen := map[MetricCategory]bool{}
	var previous MetricCategory
	for index, spec := range metrics {
		if index > 0 && spec.Category != previous {
			if seen[spec.Category] {
				t.Fatalf("category %q appears in two separate runs; the list is not grouped", spec.Category)
			}
			if spec.Category.Order() < previous.Order() {
				t.Fatalf("category %q came after %q, out of display order", spec.Category, previous)
			}
		}
		seen[spec.Category] = true
		previous = spec.Category
	}

	if len(seen) < 4 {
		t.Fatalf("only %d categories are represented, want all four", len(seen))
	}
}
