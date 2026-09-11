package audience

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"vozko/domain/shared"
)

// Mirrors domain/analysis/rubric_test.go: every field's Values() must be a
// valid persisted enum, so the schema, the prompt and the database can never
// disagree about what a label is.
func TestClassificationFields_ValuesMatchDomainEnums(t *testing.T) {
	valid := map[string]func(string) bool{
		FieldSentiment: func(v string) bool { return shared.Sentiment(v).Valid() },
		FieldStance:    func(v string) bool { return Stance(v).Valid() },
		FieldIntent:    func(v string) bool { return Intent(v).Valid() },
	}
	fields := ClassificationFields()
	if len(fields) != len(valid) {
		t.Fatalf("expected %d classification fields, got %d", len(valid), len(fields))
	}
	for _, f := range fields {
		check, ok := valid[f.Key]
		if !ok {
			t.Fatalf("unknown classification field key %q", f.Key)
		}
		if len(f.Options) == 0 {
			t.Errorf("field %q has no options", f.Key)
		}
		for _, v := range f.Values() {
			if !check(v) {
				t.Errorf("field %q value %q is not valid per its domain enum", f.Key, v)
			}
		}
	}
}

// And the reverse: every enum value is describable to the model. An enum
// constant with no rubric entry is one the model can never produce, so a
// count mismatch is a missing criterion.
func TestClassificationFields_CoverEveryEnumValue(t *testing.T) {
	byKey := map[string][]string{}
	for _, f := range ClassificationFields() {
		byKey[f.Key] = f.Values()
	}
	if got := byKey[FieldSentiment]; !reflect.DeepEqual(got, shared.SentimentValues()) {
		t.Errorf("sentiment values %v != %v", got, shared.SentimentValues())
	}
	if got := byKey[FieldStance]; !reflect.DeepEqual(got, StanceValues()) {
		t.Errorf("stance values %v != %v", got, StanceValues())
	}
	if got := byKey[FieldIntent]; !reflect.DeepEqual(got, IntentValues()) {
		t.Errorf("intent values %v != %v", got, IntentValues())
	}
}

func TestSeverityDimensions_WeightsSumToOne(t *testing.T) {
	if err := shared.WeightsSumToOne(SeverityDimensions()); err != nil {
		t.Fatal(err)
	}
	keys := []string{}
	for _, d := range SeverityDimensions() {
		keys = append(keys, d.Key)
	}
	want := []string{SeverityKeyToxicity, SeverityKeyPersonalAttack, SeverityKeyLegalRisk}
	if !reflect.DeepEqual(keys, want) {
		t.Fatalf("dimension keys = %v, want %v", keys, want)
	}
}

func sampleTopics() TopicSet {
	return TopicSet{{Key: "saude", Label: "Saúde"}, {Key: "asfalto", Label: "Asfalto"}, OtherTopic()}
}

// The response schema is generated from the rubric and the container's topic
// set: enum arrays are the rubric's Values(), the topic enum is the set's
// keys, every property is required and nothing extra is allowed. It is what
// makes ResponseFormatJSONSchema{Strict} reject an out-of-set label at the
// provider, before it ever reaches Validate.
func TestBatchResponseSchema(t *testing.T) {
	schema := BatchResponseSchema(sampleTopics())

	// Must be a plain JSON document (the AI port carries map[string]any).
	if _, err := json.Marshal(schema); err != nil {
		t.Fatalf("schema is not JSON-serialisable: %v", err)
	}

	root := schema
	if root["type"] != "object" || root["additionalProperties"] != false {
		t.Fatalf("root must be a closed object: %v", root)
	}
	props := root["properties"].(map[string]any)
	results := props[SchemaKeyResults].(map[string]any)
	if results["type"] != "array" {
		t.Fatalf("results must be an array")
	}
	item := results["items"].(map[string]any)
	if item["additionalProperties"] != false {
		t.Fatal("item must be a closed object")
	}
	itemProps := item["properties"].(map[string]any)

	enumOf := func(key string) []string {
		p, ok := itemProps[key].(map[string]any)
		if !ok {
			t.Fatalf("missing property %q", key)
		}
		e, _ := p["enum"].([]string)
		return e
	}
	for _, f := range ClassificationFields() {
		if got := enumOf(f.Key); !reflect.DeepEqual(got, f.Values()) {
			t.Errorf("%s enum = %v, want %v", f.Key, got, f.Values())
		}
	}
	for _, d := range SeverityDimensions() {
		if got := enumOf(d.Key); !reflect.DeepEqual(got, shared.QualityLevelValues()) {
			t.Errorf("%s enum = %v, want %v", d.Key, got, shared.QualityLevelValues())
		}
	}
	if got := enumOf(FieldTopicKey); !reflect.DeepEqual(got, sampleTopics().Keys()) {
		t.Errorf("topic enum = %v, want %v", got, sampleTopics().Keys())
	}
	if p := itemProps[FieldRef].(map[string]any); p["type"] != "integer" {
		t.Errorf("ref must be an integer, got %v", p)
	}
	if p := itemProps[FieldIsSpam].(map[string]any); p["type"] != "boolean" {
		t.Errorf("is_spam must be a boolean, got %v", p)
	}
	if p := itemProps[FieldLanguage].(map[string]any); p["type"] != "string" {
		t.Errorf("language must be a string, got %v", p)
	}

	// Strict mode requires every property to be listed as required.
	required, _ := item["required"].([]string)
	for key := range itemProps {
		found := false
		for _, r := range required {
			if r == key {
				found = true
			}
		}
		if !found {
			t.Errorf("property %q is not required; strict schemas need every property required", key)
		}
	}
	if len(required) != len(itemProps) {
		t.Errorf("required lists %d keys, properties has %d", len(required), len(itemProps))
	}
}

// The schema's field keys are the JSON keys BatchResult decodes; pin the
// pairing so a rename on one side cannot silently orphan the other.
func TestBatchResponseSchema_KeysMatchResultDecoding(t *testing.T) {
	payload := `{"results":[{"ref":1,"sentiment":"negative","stance":"critic","intent":"complaint",
		"topic_key":"saude","is_spam":false,"language":"pt",
		"toxicity":"low","personal_attack":"none","legal_risk":"none"}]}`
	var out BatchResponse
	if err := json.Unmarshal([]byte(payload), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Results) != 1 {
		t.Fatalf("decoded %d results", len(out.Results))
	}
	r := out.Results[0]
	if r.Ref != 1 || r.Sentiment != "negative" || r.Stance != "critic" || r.Intent != "complaint" ||
		r.TopicKey != "saude" || r.IsSpam || r.Language != "pt" ||
		r.Toxicity != "low" || r.PersonalAttack != "none" || r.LegalRisk != "none" {
		t.Fatalf("decoded result mismatch: %+v", r)
	}
	c := r.Classification()
	if err := c.Validate(sampleTopics()); err != nil {
		t.Fatalf("known-good payload rejected: %v", err)
	}
	// And an out-of-set label is rejected on the way in.
	r.Stance = "hater"
	bad := r.Classification()
	if err := bad.Validate(sampleTopics()); err == nil {
		t.Fatal("out-of-set stance must be rejected")
	}
}

// The prompt is rendered from the same rubric; a criterion edited in one
// place shows up in the prompt without any second copy to update.
func TestRubricPrompt_RendersFromFields(t *testing.T) {
	p := RubricPrompt(sampleTopics())
	for _, f := range ClassificationFields() {
		for _, o := range f.Options {
			if !contains(p, "\""+o.Value+"\"") {
				t.Errorf("prompt is missing option %q of %s", o.Value, f.Key)
			}
		}
	}
	for _, d := range SeverityDimensions() {
		if !contains(p, d.Key) {
			t.Errorf("prompt is missing severity dimension %s", d.Key)
		}
	}
	for _, tp := range sampleTopics() {
		if !contains(p, "\""+tp.Key+"\"") {
			t.Errorf("prompt is missing topic %q", tp.Key)
		}
	}
	if contains(p, "0-100") || contains(p, "0 a 100") {
		// The model must never be asked for the number.
		t.Error("prompt must not ask the model for a numeric severity")
	}
}

func contains(s, sub string) bool { return strings.Contains(s, sub) }
