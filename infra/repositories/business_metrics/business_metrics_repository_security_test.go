package business_metrics_repository

import "testing"

func TestAllowedBusinessMetricSortFields_RejectsInjection(t *testing.T) {
	t.Parallel()

	bad := []string{
		"",
		"occurred_at; DROP TABLE business_metrics;--",
		"occurred_at --",
		"(SELECT 1)",
		"occurred_at, (SELECT pg_sleep(5))",
		"1",
		"occurred_at OR 1=1",
		"random_column",
	}
	for _, name := range bad {
		if _, ok := allowedBusinessMetricSortFields[name]; ok {
			t.Errorf("allowlist unexpectedly contains %q", name)
		}
	}
}

func TestAllowedBusinessMetricSortFields_KnownGoodMaps(t *testing.T) {
	t.Parallel()

	expected := map[string]string{
		"occurred_at": "occurred_at",
		"occurredat":  "occurred_at",
		"created_at":  "created_at",
		"createdat":   "created_at",
		"event_type":  "event_type",
		"eventtype":   "event_type",
		"entity_type": "entity_type",
		"entitytype":  "entity_type",
		"user_id":     "user_id",
		"userid":      "user_id",
	}
	for key, want := range expected {
		got, ok := allowedBusinessMetricSortFields[key]
		if !ok {
			t.Errorf("missing sort key %q", key)
			continue
		}
		if got != want {
			t.Errorf("sort key %q: want %q, got %q", key, want, got)
		}
	}
}
