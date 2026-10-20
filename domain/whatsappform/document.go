package whatsappform

const DocumentSchema = 1

type Category string

const (
	CategorySignUp             Category = "SIGN_UP"
	CategorySignIn             Category = "SIGN_IN"
	CategoryAppointmentBooking Category = "APPOINTMENT_BOOKING"
	CategoryLeadGeneration     Category = "LEAD_GENERATION"
	CategoryContactUs          Category = "CONTACT_US"
	CategoryCustomerSupport    Category = "CUSTOMER_SUPPORT"
	CategorySurvey             Category = "SURVEY"
	CategoryOther              Category = "OTHER"
)

var Categories = []Category{
	CategorySignUp, CategorySignIn, CategoryAppointmentBooking, CategoryLeadGeneration,
	CategoryContactUs, CategoryCustomerSupport, CategorySurvey, CategoryOther,
}

func (c Category) Valid() bool {
	for _, known := range Categories {
		if c == known {
			return true
		}
	}
	return false
}

type DisplayKind string

const (
	DisplayHeading    DisplayKind = "heading"
	DisplaySubheading DisplayKind = "subheading"
	DisplayParagraph  DisplayKind = "paragraph"
	DisplayCaption    DisplayKind = "caption"
	DisplayImage      DisplayKind = "image"
	DisplayLink       DisplayKind = "link"
)

var DisplayKinds = []DisplayKind{DisplayHeading, DisplaySubheading, DisplayParagraph, DisplayCaption, DisplayImage, DisplayLink}

type FieldKind string

const (
	FieldShortText  FieldKind = "short_text"
	FieldEmail      FieldKind = "email"
	FieldPhone      FieldKind = "phone"
	FieldNumber     FieldKind = "number"
	FieldLongText   FieldKind = "long_text"
	FieldRadio      FieldKind = "single_choice_radio"
	FieldDropdown   FieldKind = "single_choice_dropdown"
	FieldCheckbox   FieldKind = "multi_choice_checkbox"
	FieldMultiChips FieldKind = "multi_choice_chips"
	FieldDate       FieldKind = "date"
	FieldDateRange  FieldKind = "date_range"
	FieldOptIn      FieldKind = "opt_in"
	FieldPhoto      FieldKind = "photo"
	FieldDocument   FieldKind = "document"
)

var FieldKinds = []FieldKind{
	FieldShortText, FieldEmail, FieldPhone, FieldNumber, FieldLongText,
	FieldRadio, FieldDropdown, FieldCheckbox, FieldMultiChips,
	FieldDate, FieldDateRange, FieldOptIn, FieldPhoto, FieldDocument,
}

func (k FieldKind) Valid() bool {
	for _, known := range FieldKinds {
		if k == known {
			return true
		}
	}
	return false
}

func (k FieldKind) IsSingleChoice() bool {
	return k == FieldRadio || k == FieldDropdown
}

func (k FieldKind) IsMultiChoice() bool {
	return k == FieldCheckbox || k == FieldMultiChips
}

func (k FieldKind) HasOptions() bool {
	return k.IsSingleChoice() || k.IsMultiChoice()
}

func (k FieldKind) IsTextInput() bool {
	switch k {
	case FieldShortText, FieldEmail, FieldPhone, FieldNumber:
		return true
	}
	return false
}

func (k FieldKind) IsMedia() bool {
	return k == FieldPhoto || k == FieldDocument
}

func (k FieldKind) AlwaysSensitive() bool {
	return k.IsMedia()
}

type Document struct {
	Schema     int        `json:"schema"`
	Categories []Category `json:"categories"`
	Screens    []Screen   `json:"screens"`
}

type Screen struct {
	Key         string  `json:"key"`
	Title       string  `json:"title"`
	Blocks      []Block `json:"blocks"`
	Route       Route   `json:"route"`
	SubmitLabel string  `json:"submitLabel"`
}

type Route struct {
	Next     string   `json:"next,omitempty"`
	BranchOn string   `json:"branchOn,omitempty"`
	Branches []Branch `json:"branches,omitempty"`
}

type Branch struct {
	OptionID string `json:"optionId"`
	Next     string `json:"next"`
}

func (r Route) IsTerminal() bool {
	return r.Next == "" && r.BranchOn == "" && len(r.Branches) == 0
}

func (r Route) Targets() []string {
	var targets []string
	seen := map[string]bool{}
	add := func(key string) {
		if key != "" && !seen[key] {
			seen[key] = true
			targets = append(targets, key)
		}
	}
	add(r.Next)
	for _, b := range r.Branches {
		add(b.Next)
	}
	return targets
}

type Block struct {
	Key       string     `json:"key"`
	Display   *Display   `json:"display,omitempty"`
	Field     *Field     `json:"field,omitempty"`
	VisibleIf *Condition `json:"visibleIf,omitempty"`
}

type Display struct {
	Kind    DisplayKind `json:"kind"`
	Text    string      `json:"text,omitempty"`
	MediaID string      `json:"mediaId,omitempty"`
	URL     string      `json:"url,omitempty"`
}

type Field struct {
	Key       string    `json:"key"`
	Kind      FieldKind `json:"kind"`
	Question  string    `json:"question,omitempty"`
	Label     string    `json:"label"`
	Help      string    `json:"help,omitempty"`
	Required  bool      `json:"required"`
	Sensitive bool      `json:"sensitive"`
	Options   []Option  `json:"options,omitempty"`
	Min       int       `json:"min,omitempty"`
	Max       int       `json:"max,omitempty"`
	Pattern   string    `json:"pattern,omitempty"`
	MinDate   string    `json:"minDate,omitempty"`
	MaxDate   string    `json:"maxDate,omitempty"`
	MimeTypes []string  `json:"mimeTypes,omitempty"`
	TermsURL  string    `json:"termsUrl,omitempty"`
	Binding   *Binding  `json:"binding,omitempty"`
}

func (f Field) IsSensitive() bool {
	return f.Sensitive || f.Kind.AlwaysSensitive()
}

func (f Field) Option(id string) (Option, bool) {
	for _, o := range f.Options {
		if o.ID == id {
			return o, true
		}
	}
	return Option{}, false
}

type Option struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
}

type Condition struct {
	FieldKey string `json:"fieldKey"`
	Value    string `json:"value"`
	Negate   bool   `json:"negate,omitempty"`
}

type FieldRef struct {
	Screen int
	Block  int
	Field  Field
}

func (d Document) Fields() []FieldRef {
	var refs []FieldRef
	for si, s := range d.Screens {
		for bi, b := range s.Blocks {
			if b.Field != nil {
				refs = append(refs, FieldRef{Screen: si, Block: bi, Field: *b.Field})
			}
		}
	}
	return refs
}

func (d Document) Field(key string) (FieldRef, bool) {
	for _, ref := range d.Fields() {
		if ref.Field.Key == key {
			return ref, true
		}
	}
	return FieldRef{}, false
}

func (d Document) ScreenIndex(key string) int {
	for i, s := range d.Screens {
		if s.Key == key {
			return i
		}
	}
	return -1
}

func (d Document) MediaIDs() []string {
	var ids []string
	seen := map[string]bool{}
	for _, s := range d.Screens {
		for _, b := range s.Blocks {
			if b.Display != nil && b.Display.Kind == DisplayImage && b.Display.MediaID != "" && !seen[b.Display.MediaID] {
				seen[b.Display.MediaID] = true
				ids = append(ids, b.Display.MediaID)
			}
		}
	}
	return ids
}

func MissingAssets(d Document, assets Assets) Problems {
	var problems Problems
	for _, s := range d.Screens {
		for _, b := range s.Blocks {
			if b.Display == nil || b.Display.Kind != DisplayImage || b.Display.MediaID == "" {
				continue
			}
			if _, ok := assets[b.Display.MediaID]; !ok {
				problems = append(problems, Problem{Screen: s.Key, Block: b.Key, Property: "mediaId", Code: ProblemUnknownTarget})
			}
		}
	}
	return problems
}
