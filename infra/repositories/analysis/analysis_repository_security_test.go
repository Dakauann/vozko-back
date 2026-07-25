package analysis_repository

import "testing"

func TestAllowedAnalysisSortFields_RejectsInjection(t *testing.T) {
	t.Parallel()

	bad := []string{
		"",
		"created_at; DROP TABLE analyses;--",
		"(SELECT 1)",
		"created_at OR 1=1",
		"pg_sleep(5)",
		"random_column",
		"CREATED_AT",
	}
	for _, name := range bad {
		if _, ok := allowedAnalysisSortFields[name]; ok {
			t.Errorf("allowlist unexpectedly contains %q", name)
		}
	}
}

func TestAllowedAnalysisSortFields_KnownGoodMaps(t *testing.T) {
	t.Parallel()

	expected := map[string]string{
		"created_at":         "created_at",
		"createdat":          "created_at",
		"attendance_quality": "attendance_quality",
		"attendancequality":  "attendance_quality",
		"interest":           "interest",
		"disposition":        "disposition",
		"sentiment":          "sentiment",
		"qualification":      "qualification",
	}
	for key, want := range expected {
		got, ok := allowedAnalysisSortFields[key]
		if !ok {
			t.Errorf("missing sort key %q", key)
			continue
		}
		if got != want {
			t.Errorf("sort key %q: want %q, got %q", key, want, got)
		}
	}
}
