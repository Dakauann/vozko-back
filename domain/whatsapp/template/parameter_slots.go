package template

import "strconv"

type ParameterSlot struct {
	Name    string
	Header  bool
	Example string
}

func (t *Template) ParameterSlots() []ParameterSlot {
	body, header := t.GetBodyAndHeaderParameterNames()
	slots := make([]ParameterSlot, 0, len(body)+len(header))
	slots = append(slots, componentSlots(header, true, t.componentExample("HEADER"))...)
	return append(slots, componentSlots(body, false, t.componentExample("BODY"))...)
}

func (t *Template) SplitParameters(values []string) (body []string, header []string) {
	slots := t.ParameterSlots()
	for i, value := range values {
		if i < len(slots) && slots[i].Header {
			header = append(header, value)
			continue
		}
		body = append(body, value)
	}
	return body, header
}

func (t *Template) componentExample(kind string) *TemplateExample {
	for _, c := range t.Components {
		if c.Type == kind && (kind != "HEADER" || c.Format == "TEXT") {
			return c.Example
		}
	}
	return nil
}

func componentSlots(names []string, header bool, example *TemplateExample) []ParameterSlot {
	positional := positionalCount(names)
	if positional > 0 {
		slots := make([]ParameterSlot, 0, positional)
		for n := 1; n <= positional; n++ {
			slots = append(slots, ParameterSlot{Name: strconv.Itoa(n), Header: header, Example: positionalExample(example, header, n-1)})
		}
		return slots
	}
	slots := make([]ParameterSlot, 0, len(names))
	for _, name := range names {
		slots = append(slots, ParameterSlot{Name: name, Header: header, Example: namedExample(example, header, name)})
	}
	return slots
}

func positionalCount(names []string) int {
	highest := 0
	for _, name := range names {
		n, err := strconv.Atoi(name)
		if err != nil {
			return 0
		}
		highest = max(highest, n)
	}
	return highest
}

func positionalExample(example *TemplateExample, header bool, i int) string {
	if example == nil {
		return ""
	}
	values := example.HeaderText
	if !header {
		values = nil
		if len(example.BodyText) > 0 {
			values = example.BodyText[0]
		}
	}
	if i < len(values) {
		return values[i]
	}
	return ""
}

func namedExample(example *TemplateExample, header bool, name string) string {
	if example == nil {
		return ""
	}
	named := example.BodyTextNamed
	if header {
		named = example.HeaderTextNamed
	}
	for _, e := range named {
		if e.ParamName == name {
			return e.Example
		}
	}
	return ""
}
