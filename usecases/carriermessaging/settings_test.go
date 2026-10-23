package carriermessaging_usecase

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/carriermessaging"
)

func TestSettingsDefaultUntilSaved(t *testing.T) {
	h := newHarness(t)
	s, err := h.svc.GetSettings(context.Background(), "ws-1")
	if err != nil || s.FrequencyCap != carriermessaging.DefaultFrequencyCap() || s.QuietHours.Timezone != "America/Sao_Paulo" {
		t.Fatalf("GetSettings = %+v, %v", s, err)
	}
	s.FrequencyCap = carriermessaging.FrequencyCap{PerDay: 1, PerWeek: 3}
	if _, err := h.svc.PutSettings(context.Background(), "ws-1", s); err != nil {
		t.Fatalf("PutSettings = %v", err)
	}
	got, _ := h.svc.GetSettings(context.Background(), "ws-1")
	if got.FrequencyCap.PerWeek != 3 || got.WorkspaceID != "ws-1" {
		t.Fatalf("saved = %+v", got)
	}
	s.FrequencyCap = carriermessaging.FrequencyCap{PerDay: 5, PerWeek: 1}
	if _, err := h.svc.PutSettings(context.Background(), "ws-1", s); !errors.Is(err, carriermessaging.ErrSettingsInvalid) {
		t.Fatalf("invalid = %v", err)
	}
}

func TestSuppressionsByOperator(t *testing.T) {
	h := newHarness(t)
	if err := h.svc.AddSuppression(context.Background(), "ws-1", "+55 (11) 99999-0000"); err != nil {
		t.Fatalf("AddSuppression = %v", err)
	}
	list, total, err := h.svc.ListSuppressions(context.Background(), "ws-1", 50, 0)
	if err != nil || total != 1 || list[0].Phone != "5511999990000" || list[0].Source != carriermessaging.SuppressionOperator {
		t.Fatalf("list = %+v, %d, %v", list, total, err)
	}
	if err := h.svc.RemoveSuppression(context.Background(), "ws-1", "5511999990000"); err != nil {
		t.Fatalf("RemoveSuppression = %v", err)
	}
	if err := h.svc.AddSuppression(context.Background(), "ws-1", "12"); !errors.Is(err, carriermessaging.ErrPhoneInvalid) {
		t.Fatalf("bad phone = %v", err)
	}
}
