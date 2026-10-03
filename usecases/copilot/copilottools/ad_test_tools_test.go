package copilottools

import (
	"context"
	"errors"
	"testing"

	"vozko/domain/advertising"
	"vozko/domain/copilot"
)

type stubSplitTests struct {
	objects map[string]*advertising.Object
	created *advertising.SplitTest
}

var stubTestLevels = map[advertising.TestLevel]advertising.Level{advertising.TestCampaigns: advertising.LevelCampaign, advertising.TestAdSets: advertising.LevelAdSet}

func (s *stubSplitTests) Check(_ context.Context, _ string, test advertising.SplitTest) (advertising.SplitTest, error) {
	test.Normalize()
	for i := range test.Cells {
		for _, id := range test.Cells[i].ObjectIDs {
			o, ok := s.objects[id]
			if !ok || o.AdAccountID != test.AdAccountID || o.Level != stubTestLevels[test.Level] {
				return advertising.SplitTest{}, advertising.FieldError("cells.objectIds", "not_available")
			}
			if test.Cells[i].Name == "" {
				test.Cells[i].Name = o.Name
			}
		}
	}
	return test, test.Validate(adTestClock)
}

func (s *stubSplitTests) List(context.Context, string, string) ([]advertising.SplitTest, error) {
	return []advertising.SplitTest{{MetaID: "880", Name: "A contra B", Level: advertising.TestAdSets, StartAt: adTestClock, EndAt: adTestClock.AddDate(0, 0, 7), Confidence: 90,
		Cells: []advertising.TestCell{{Name: "Conjunto A", Share: 50, ObjectIDs: []string{"120300"}}, {Name: "Conjunto B", Share: 50, ObjectIDs: []string{"120301"}}}}}, nil
}
func (s *stubSplitTests) Create(_ context.Context, _ string, test advertising.SplitTest) (string, error) {
	s.created = &test
	return "881", nil
}

func splitTestArgsMap() map[string]interface{} {
	return map[string]interface{}{
		"ad_account_id": adAccountUUID, "name": "A contra B", "level": "adset",
		"object_ids": []interface{}{"120300", "120301"}, "end_date": "2026-10-08",
	}
}

func TestABTestSplitsTheAudienceEvenlyBetweenListedItems(t *testing.T) {
	tools, s := growthTools()
	tool := tools["create_ad_test"]
	if err := growthValidate(tool, splitTestArgsMap()); err != nil {
		t.Fatal(err)
	}
	if fields := growthDescribe(tool, splitTestArgsMap()); fields["versions"] != "Conjunto A, Conjunto B" || fields["period"] != "01/10/2026 a 08/10/2026" || fields["confidence"] != "90%" {
		t.Fatalf("fields %+v", fields)
	}
	result := tool.Execute(context.Background(), adContext, splitTestArgsMap())
	test := s.tests.created
	if result.Status != copilot.StatusOK || test.Level != advertising.TestAdSets || test.Cells[0].Share+test.Cells[1].Share != 100 ||
		test.Cells[0].ObjectIDs[0] != "120300" || !test.StartAt.After(adTestClock) {
		t.Fatalf("result %+v test %+v", result, test)
	}
}

func TestABTestRefusesOneVersionUnknownItemsOrTooLong(t *testing.T) {
	tools, _ := growthTools()
	tool := tools["create_ad_test"]
	for name, change := range map[string]func(map[string]interface{}){
		"one version":  func(a map[string]interface{}) { a["object_ids"] = []interface{}{"120300"} },
		"unknown item": func(a map[string]interface{}) { a["object_ids"] = []interface{}{"120300", "999"} },
		"too long":     func(a map[string]interface{}) { a["end_date"] = "2026-12-01" },
		"bad level":    func(a map[string]interface{}) { a["object_ids"] = []interface{}{"120200", "120300"} },
	} {
		args := splitTestArgsMap()
		change(args)
		if err := growthValidate(tool, args); !errors.Is(err, errInvalidArgs) {
			t.Fatalf("%s: err %v", name, err)
		}
	}
}

func TestListABTestsShowsVersionsAndPeriod(t *testing.T) {
	tools, _ := growthTools()
	result := tools["list_ad_tests"].Execute(context.Background(), adContext, map[string]interface{}{"ad_account_id": adAccountUUID})
	tests := result.Data.(map[string]interface{})["tests"].([]map[string]interface{})
	if result.Status != copilot.StatusOK || tests[0]["test_id"] != "880" || len(tests[0]["cells"].([]map[string]interface{})) != 2 {
		t.Fatalf("result %+v", result)
	}
}
