package whatsappform

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

var FirstScreenID = screenID(0)

const (
	FlowJSONVersion = "7.3"
	CompilerVersion = "1"
)

var ErrAssetMissing = errors.New("whatsappform: an image of the form is missing from storage")

type Assets map[string][]byte

type Compiled struct {
	JSON        []byte
	Hash        string
	FlowVersion string
	SourceMap   SourceMap
}

type obj = map[string]any

type compiler struct {
	doc      Document
	assets   Assets
	sources  SourceMap
	problems Problems
	screenOf map[string]int
}

func Compile(doc Document, assets Assets) (Compiled, error) {
	if err := ValidateStructure(doc).Err(); err != nil {
		return Compiled{}, err
	}
	c := &compiler{doc: doc, assets: assets, sources: SourceMap{}, screenOf: map[string]int{}}
	for _, ref := range doc.Fields() {
		c.screenOf[ref.Field.Key] = ref.Screen
	}
	screens := make([]any, 0, len(doc.Screens))
	for i := range doc.Screens {
		screen, err := c.screen(i)
		if err != nil {
			return Compiled{}, err
		}
		screens = append(screens, screen)
	}
	if err := c.problems.Err(); err != nil {
		return Compiled{}, err
	}
	encoded, err := encode(obj{"version": FlowJSONVersion, "screens": screens})
	if err != nil {
		return Compiled{}, err
	}
	sum := sha256.Sum256(encoded)
	return Compiled{JSON: encoded, Hash: hex.EncodeToString(sum[:]), FlowVersion: FlowJSONVersion, SourceMap: c.sources}, nil
}

func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}

func screenID(index int) string {
	var letters []byte
	for n := index; ; n = n/26 - 1 {
		letters = append([]byte{byte('A' + n%26)}, letters...)
		if n < 26 {
			break
		}
	}
	return "SCREEN_" + string(letters)
}

func (c *compiler) screenIDOf(key string) string {
	return screenID(c.doc.ScreenIndex(key))
}

func (c *compiler) screen(index int) (obj, error) {
	s := c.doc.Screens[index]
	c.sources.add(fmt.Sprintf("screens/%d", index), s.Key, "")
	var children []any
	var sensitive []string
	for _, b := range s.Blocks {
		components, err := c.block(index, s.Key, b)
		if err != nil {
			return nil, err
		}
		for _, component := range components {
			c.sources.add(fmt.Sprintf("screens/%d/layout/children/%d", index, len(children)), s.Key, b.Key)
			children = append(children, component)
		}
		if b.Field != nil && b.Field.IsSensitive() {
			sensitive = append(sensitive, b.Field.Key)
		}
	}
	children = append(children, c.footerOrSwitch(index))
	out := obj{
		"id":     screenID(index),
		"title":  s.Title,
		"layout": obj{"type": "SingleColumnLayout", "children": children},
	}
	if s.Route.IsTerminal() {
		out["terminal"] = true
	}
	if len(sensitive) > 0 {
		out["sensitive"] = sensitive
	}
	return out, nil
}

func (c *compiler) ref(currentScreen int, key string) string {
	if c.screenOf[key] == currentScreen {
		return "${form." + key + "}"
	}
	return "${screen." + screenID(c.screenOf[key]) + ".form." + key + "}"
}

func (c *compiler) visible(currentScreen int, cond *Condition) string {
	ref := c.ref(currentScreen, cond.FieldKey)
	field, _ := c.doc.Field(cond.FieldKey)
	if field.Field.Kind == FieldOptIn {
		checked := cond.Value == "true"
		if cond.Negate {
			checked = !checked
		}
		if checked {
			return ref
		}
		return "`!" + ref + "`"
	}
	op := "=="
	if cond.Negate {
		op = "!="
	}
	return "`" + ref + " " + op + " '" + cond.Value + "'`"
}

func (c *compiler) block(screenIndex int, screenKey string, b Block) ([]obj, error) {
	var components []obj
	if b.Display != nil {
		component, err := c.display(screenKey, b)
		if err != nil {
			return nil, err
		}
		if component == nil {
			return nil, nil
		}
		components = append(components, component)
	} else {
		if !blank(b.Field.Question) {
			components = append(components, obj{"type": "TextBody", "text": b.Field.Question})
		}
		components = append(components, fieldComponent(*b.Field))
	}
	if b.VisibleIf != nil {
		expression := c.visible(screenIndex, b.VisibleIf)
		for _, component := range components {
			component["visible"] = expression
		}
	}
	return components, nil
}

var textComponents = map[DisplayKind]string{
	DisplayHeading:    "TextHeading",
	DisplaySubheading: "TextSubheading",
	DisplayParagraph:  "TextBody",
	DisplayCaption:    "TextCaption",
}

func (c *compiler) display(screenKey string, b Block) (obj, error) {
	d := b.Display
	switch d.Kind {
	case DisplayImage:
		data, ok := c.assets[d.MediaID]
		if !ok {
			return nil, fmt.Errorf("%w: %s", ErrAssetMissing, d.MediaID)
		}
		if len(data) > MaxImageBytes {
			c.problems = append(c.problems, Problem{Screen: screenKey, Block: b.Key, Property: "mediaId", Code: ProblemTooLong, Limit: MaxImageBytes})
			return nil, nil
		}
		image := obj{"type": "Image", "src": base64.StdEncoding.EncodeToString(data), "scale-type": "contain"}
		if !blank(d.Text) {
			image["alt-text"] = d.Text
		}
		return image, nil
	case DisplayLink:
		return obj{"type": "EmbeddedLink", "text": d.Text, "on-click-action": openURL(d.URL)}, nil
	}
	return obj{"type": textComponents[d.Kind], "text": d.Text}, nil
}

func openURL(u string) obj {
	return obj{"name": "open_url", "url": strings.TrimSpace(u)}
}

var inputTypes = map[FieldKind]string{
	FieldShortText: "text",
	FieldEmail:     "email",
	FieldPhone:     "phone",
	FieldNumber:    "number",
}

var choiceComponents = map[FieldKind]string{
	FieldRadio:      "RadioButtonsGroup",
	FieldDropdown:   "Dropdown",
	FieldCheckbox:   "CheckboxGroup",
	FieldMultiChips: "ChipsSelector",
}

func setIf(o obj, key string, value any, present bool) {
	if present {
		o[key] = value
	}
}

func fieldComponent(f Field) obj {
	base := func(kind string) obj {
		return obj{"type": kind, "name": f.Key, "label": f.Label}
	}
	switch {
	case f.Kind.IsTextInput():
		o := base("TextInput")
		o["input-type"], o["required"] = inputTypes[f.Kind], f.Required
		setIf(o, "min-chars", f.Min, f.Min > 0)
		setIf(o, "max-chars", f.Max, f.Max > 0)
		setIf(o, "helper-text", f.Help, !blank(f.Help))
		setIf(o, "pattern", f.Pattern, f.Pattern != "")
		return o
	case f.Kind == FieldLongText:
		o := base("TextArea")
		o["required"] = f.Required
		setIf(o, "max-length", f.Max, f.Max > 0)
		setIf(o, "helper-text", f.Help, !blank(f.Help))
		return o
	case f.Kind.HasOptions():
		o := base(choiceComponents[f.Kind])
		o["required"], o["data-source"] = f.Required, dataSource(f.Options)
		setIf(o, "description", f.Help, !blank(f.Help) && f.Kind != FieldDropdown)
		if f.Kind.IsMultiChoice() {
			setIf(o, "min-selected-items", f.Min, f.Min > 0)
			setIf(o, "max-selected-items", f.Max, f.Max > 0)
		}
		return o
	case f.Kind == FieldDate, f.Kind == FieldDateRange:
		o := base("DatePicker")
		if f.Kind == FieldDateRange {
			o = base("CalendarPicker")
			o["mode"] = "range"
		}
		o["required"] = f.Required
		setIf(o, "min-date", f.MinDate, f.MinDate != "")
		setIf(o, "max-date", f.MaxDate, f.MaxDate != "")
		setIf(o, "helper-text", f.Help, !blank(f.Help))
		return o
	case f.Kind == FieldOptIn:
		o := base("OptIn")
		o["required"] = f.Required
		setIf(o, "on-click-action", openURL(f.TermsURL), f.TermsURL != "")
		return o
	}
	return picker(f, base)
}

func picker(f Field, base func(string) obj) obj {
	minimum, maximum := f.Min, f.Max
	if f.Required && minimum == 0 {
		minimum = 1
	}
	if maximum == 0 {
		maximum = max(1, minimum)
	}
	if f.Kind == FieldPhoto {
		o := base("PhotoPicker")
		o["photo-source"], o["min-uploaded-photos"], o["max-uploaded-photos"] = "camera_gallery", minimum, maximum
		setIf(o, "description", f.Help, !blank(f.Help))
		return o
	}
	o := base("DocumentPicker")
	o["min-uploaded-documents"], o["max-uploaded-documents"] = minimum, maximum
	setIf(o, "description", f.Help, !blank(f.Help))
	setIf(o, "allowed-mime-types", f.MimeTypes, len(f.MimeTypes) > 0)
	return o
}

func dataSource(options []Option) []obj {
	out := make([]obj, 0, len(options))
	for _, o := range options {
		item := obj{"id": o.ID, "title": o.Title}
		setIf(item, "description", o.Description, !blank(o.Description))
		out = append(out, item)
	}
	return out
}

func (c *compiler) footerOrSwitch(index int) obj {
	s := c.doc.Screens[index]
	if s.Route.IsTerminal() {
		return footer(s.SubmitLabel, obj{"name": "complete", "payload": c.completePayload(index)})
	}
	if s.Route.BranchOn == "" {
		return c.navigateFooter(s.SubmitLabel, s.Route.Next)
	}
	field, _ := c.doc.Field(s.Route.BranchOn)
	targets := map[string]string{}
	for _, b := range s.Route.Branches {
		targets[b.OptionID] = b.Next
	}
	cases := obj{}
	for _, o := range field.Field.Options {
		next, ok := targets[o.ID]
		if !ok {
			next = s.Route.Next
		}
		cases[o.ID] = []any{c.navigateFooter(s.SubmitLabel, next)}
	}
	return obj{"type": "Switch", "value": c.ref(index, s.Route.BranchOn), "cases": cases}
}

func (c *compiler) navigateFooter(label, next string) obj {
	return footer(label, obj{"name": "navigate", "next": obj{"type": "screen", "name": c.screenIDOf(next)}, "payload": obj{}})
}

func footer(label string, action obj) obj {
	return obj{"type": "Footer", "label": label, "on-click-action": action}
}

func (c *compiler) completePayload(index int) obj {
	payload := obj{}
	for _, ref := range c.doc.Fields() {
		payload[ref.Field.Key] = c.ref(index, ref.Field.Key)
	}
	return payload
}
