package advertising

import (
	"errors"
	"slices"
	"strings"
	"time"
)

var ErrLeadFormNotFound = errors.New("lead form not found")

type QuestionType string

const (
	QuestionFullName  QuestionType = "FULL_NAME"
	QuestionFirstName QuestionType = "FIRST_NAME"
	QuestionLastName  QuestionType = "LAST_NAME"
	QuestionEmail     QuestionType = "EMAIL"
	QuestionPhone     QuestionType = "PHONE"
	QuestionCity      QuestionType = "CITY"
	QuestionState     QuestionType = "STATE"
	QuestionZip       QuestionType = "ZIP"
	QuestionCompany   QuestionType = "COMPANY_NAME"
	QuestionJobTitle  QuestionType = "JOB_TITLE"
	QuestionCustom    QuestionType = "CUSTOM"
)

var standardQuestions = []QuestionType{QuestionFullName, QuestionFirstName, QuestionLastName, QuestionEmail, QuestionPhone, QuestionCity, QuestionState, QuestionZip, QuestionCompany, QuestionJobTitle}

type FormQuestion struct {
	Type    QuestionType `json:"type"`
	Key     string       `json:"key,omitempty"`
	Label   string       `json:"label,omitempty"`
	Options []string     `json:"options,omitempty"`
}

type FormStatus string

const (
	FormActive   FormStatus = "ACTIVE"
	FormArchived FormStatus = "ARCHIVED"
)

type LeadForm struct {
	MetaID      string         `json:"metaId"`
	PageID      string         `json:"pageId"`
	Name        string         `json:"name"`
	Status      FormStatus     `json:"status"`
	Locale      string         `json:"locale,omitempty"`
	Questions   []FormQuestion `json:"questions"`
	PrivacyURL  string         `json:"privacyUrl,omitempty"`
	LeadsCount  int64          `json:"leadsCount"`
	CreatedTime *time.Time     `json:"createdTime,omitempty"`
}

type LeadFormDraft struct {
	AdAccountID   string         `json:"adAccountId"`
	PageID        string         `json:"pageId"`
	Name          string         `json:"name"`
	Locale        string         `json:"locale,omitempty"`
	Headline      string         `json:"headline,omitempty"`
	Questions     []FormQuestion `json:"questions"`
	PrivacyURL    string         `json:"privacyUrl"`
	PrivacyText   string         `json:"privacyText,omitempty"`
	ThankYouTitle string         `json:"thankYouTitle,omitempty"`
	ThankYouBody  string         `json:"thankYouBody,omitempty"`
	ThankYouURL   string         `json:"thankYouUrl,omitempty"`
	HigherIntent  bool           `json:"higherIntent,omitempty"`
}

const (
	maxQuestions      = 15
	maxQuestionOption = 10
	maxLabelRunes     = 80
	maxFormTextRunes  = 300
	defaultFormLocale = "pt_BR"
)

func (d *LeadFormDraft) Normalize() {
	d.Name = strings.TrimSpace(d.Name)
	if d.Locale == "" {
		d.Locale = defaultFormLocale
	}
	for i := range d.Questions {
		q := &d.Questions[i]
		q.Label = strings.TrimSpace(q.Label)
		q.Key = strings.TrimSpace(q.Key)
		if q.Type == QuestionCustom && q.Key == "" {
			q.Key = customKey(q.Label, i)
		}
		q.Options = nonBlank(q.Options)
	}
}

func customKey(label string, index int) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		r = foldAccent(r)
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '_' || r == '-':
			b.WriteRune('_')
		}
	}
	key := strings.Trim(b.String(), "_")
	if key == "" {
		key = "pergunta"
	}
	return key + "_" + string(rune('a'+index%26))
}

func (d LeadFormDraft) Validate() error {
	v := newIssues()
	if strings.TrimSpace(d.AdAccountID) == "" {
		v.add("adAccountId", "required")
	}
	if strings.TrimSpace(d.PageID) == "" {
		v.add("pageId", "required")
	}
	v.text("name", d.Name, true, maxNameRunes)
	v.text("headline", d.Headline, false, maxLabelRunes)
	v.url("privacyUrl", d.PrivacyURL, true)
	v.text("privacyText", d.PrivacyText, false, maxLabelRunes)
	v.text("thankYouTitle", d.ThankYouTitle, false, maxLabelRunes)
	v.text("thankYouBody", d.ThankYouBody, false, maxFormTextRunes)
	v.url("thankYouUrl", d.ThankYouURL, false)
	switch n := len(d.Questions); {
	case n == 0:
		v.add("questions", "required")
	case n > maxQuestions:
		v.add("questions", "too_many")
	}
	contact := false
	seen := map[string]bool{}
	for i, q := range d.Questions {
		qv := v.item("questions", i)
		contact = contact || q.Type == QuestionPhone || q.Type == QuestionEmail
		switch {
		case q.Type == QuestionCustom:
			qv.text("label", q.Label, true, maxLabelRunes)
			if len(q.Options) > maxQuestionOption {
				qv.add("options", "too_many")
			}
		case !slices.Contains(standardQuestions, q.Type):
			qv.add("type", "invalid")
		}
		id := string(q.Type) + q.Key
		if seen[id] {
			qv.add("type", "repeated")
		}
		seen[id] = true
	}
	if !contact {
		v.add("questions", "needs_phone_or_email")
	}
	return v.err()
}

type FormLead struct {
	MetaID      string            `json:"metaId"`
	FormMetaID  string            `json:"formMetaId"`
	AdMetaID    string            `json:"adMetaId,omitempty"`
	PageID      string            `json:"pageId,omitempty"`
	WorkspaceID string            `json:"-"`
	Answers     map[string]string `json:"answers"`
	LeadID      string            `json:"leadId,omitempty"`
	CreatedTime time.Time         `json:"createdTime"`
}

type LeadContact struct {
	Phone string
	Email string
	Name  string
}

func (l FormLead) Contact() LeadContact {
	a := map[string]string{}
	for k, v := range l.Answers {
		a[strings.ToLower(strings.TrimSpace(k))] = strings.TrimSpace(v)
	}
	name := a["full_name"]
	if name == "" {
		name = strings.TrimSpace(a["first_name"] + " " + a["last_name"])
	}
	phone := a["phone_number"]
	if phone == "" {
		phone = a["phone"]
	}
	return LeadContact{Phone: DigitsOnly(phone), Email: NormalizeEmail(a["email"]), Name: name}
}

var accentFolds = map[rune]rune{
	'á': 'a', 'à': 'a', 'â': 'a', 'ã': 'a', 'ä': 'a',
	'é': 'e', 'è': 'e', 'ê': 'e', 'ë': 'e',
	'í': 'i', 'ì': 'i', 'î': 'i', 'ï': 'i',
	'ó': 'o', 'ò': 'o', 'ô': 'o', 'õ': 'o', 'ö': 'o',
	'ú': 'u', 'ù': 'u', 'û': 'u', 'ü': 'u',
	'ç': 'c', 'ñ': 'n',
}

func foldAccent(r rune) rune {
	if folded, ok := accentFolds[r]; ok {
		return folded
	}
	return r
}

const (
	DefaultLeadsPage = 50
	MaxLeadsPage     = 200
	MaxLeadsOffset   = 1_000_000
)

func (q *FormLeadQuery) Page() error {
	if q.Limit == 0 {
		q.Limit = DefaultLeadsPage
	}
	if q.Limit < 1 || q.Limit > MaxLeadsPage {
		return FieldError("limit", "invalid")
	}
	if q.Offset < 0 || q.Offset > MaxLeadsOffset {
		return FieldError("offset", "invalid")
	}
	return nil
}
