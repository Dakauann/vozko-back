package emaildesign

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
)

const CurrentVersion = 1

const MaxBlocks = 200

var (
	ErrMalformedDocument = errors.New("emaildesign: malformed document")
	ErrUnknownBlockKind  = errors.New("emaildesign: unknown block kind")
)

type Document struct {
	Version  int      `json:"version"`
	Settings Settings `json:"settings"`
	Blocks   []Block  `json:"blocks"`
}

type Settings struct {
	Width                  int    `json:"width,omitempty"`
	BackgroundColor        string `json:"background_color,omitempty"`
	ContentBackgroundColor string `json:"content_background_color,omitempty"`
	TextColor              string `json:"text_color,omitempty"`
	LinkColor              string `json:"link_color,omitempty"`
	ButtonColor            string `json:"button_color,omitempty"`
	ButtonTextColor        string `json:"button_text_color,omitempty"`
	FontFamily             string `json:"font_family,omitempty"`
	Preheader              string `json:"preheader,omitempty"`
}

type BlockKind string

const (
	KindHeading BlockKind = "heading"
	KindText    BlockKind = "text"
	KindButton  BlockKind = "button"
	KindImage   BlockKind = "image"
	KindDivider BlockKind = "divider"
	KindSpacer  BlockKind = "spacer"
	KindColumns BlockKind = "columns"
	KindSocial  BlockKind = "social"
	KindFooter  BlockKind = "footer"
)

var knownKinds = []BlockKind{KindHeading, KindText, KindButton, KindImage, KindDivider, KindSpacer, KindColumns, KindSocial, KindFooter}

func (k BlockKind) Known() bool {
	for _, known := range knownKinds {
		if k == known {
			return true
		}
	}
	return false
}

func (k *BlockKind) UnmarshalJSON(raw []byte) error {
	var value string
	if err := json.Unmarshal(raw, &value); err != nil {
		return err
	}
	kind := BlockKind(value)
	if !kind.Known() {
		return fmt.Errorf("%w: %q", ErrUnknownBlockKind, value)
	}
	*k = kind
	return nil
}

type Padding string

const (
	PaddingSmall  Padding = "s"
	PaddingMedium Padding = "m"
	PaddingLarge  Padding = "l"
)

type Align string

const (
	AlignLeft   Align = "left"
	AlignCenter Align = "center"
	AlignRight  Align = "right"
)

type Block struct {
	ID              string    `json:"id"`
	Kind            BlockKind `json:"kind"`
	Padding         Padding   `json:"padding,omitempty"`
	BackgroundColor string    `json:"background_color,omitempty"`
	Heading         *Heading  `json:"heading,omitempty"`
	Text            *Text     `json:"text,omitempty"`
	Button          *Button   `json:"button,omitempty"`
	Image           *Image    `json:"image,omitempty"`
	Divider         *Divider  `json:"divider,omitempty"`
	Spacer          *Spacer   `json:"spacer,omitempty"`
	Columns         *Columns  `json:"columns,omitempty"`
	Social          *Social   `json:"social,omitempty"`
	Footer          *Footer   `json:"footer,omitempty"`
}

type Heading struct {
	Text  string `json:"text"`
	Level int    `json:"level"`
	Align Align  `json:"align,omitempty"`
	Color string `json:"color,omitempty"`
}

type Text struct {
	Paragraphs []Paragraph `json:"paragraphs"`
	Align      Align       `json:"align,omitempty"`
}

type Paragraph struct {
	Runs []Run `json:"runs"`
}

type Run struct {
	Text      string `json:"text"`
	Bold      bool   `json:"bold,omitempty"`
	Italic    bool   `json:"italic,omitempty"`
	Underline bool   `json:"underline,omitempty"`
	Link      *Link  `json:"link,omitempty"`
}

type Link struct {
	Href string `json:"href"`
}

type Button struct {
	Label     string `json:"label"`
	Href      string `json:"href"`
	Align     Align  `json:"align,omitempty"`
	FullWidth bool   `json:"full_width,omitempty"`
	Color     string `json:"color,omitempty"`
	TextColor string `json:"text_color,omitempty"`
}

type Image struct {
	Src   string `json:"src"`
	Alt   string `json:"alt"`
	Href  string `json:"href,omitempty"`
	Width int    `json:"width"`
	Align Align  `json:"align,omitempty"`
}

type Divider struct {
	Color     string `json:"color,omitempty"`
	Thickness int    `json:"thickness"`
}

type Spacer struct {
	Height int `json:"height"`
}

type ColumnRatio string

const (
	Ratio5050   ColumnRatio = "50-50"
	Ratio3367   ColumnRatio = "33-67"
	Ratio6733   ColumnRatio = "67-33"
	Ratio333333 ColumnRatio = "33-33-33"
)

type Columns struct {
	Ratio   ColumnRatio `json:"ratio"`
	Columns []Column    `json:"columns"`
}

type Column struct {
	Blocks []Block `json:"blocks"`
}

type SocialNetwork string

const (
	NetworkFacebook  SocialNetwork = "facebook"
	NetworkInstagram SocialNetwork = "instagram"
	NetworkLinkedIn  SocialNetwork = "linkedin"
	NetworkX         SocialNetwork = "x"
	NetworkYouTube   SocialNetwork = "youtube"
	NetworkTikTok    SocialNetwork = "tiktok"
	NetworkWhatsApp  SocialNetwork = "whatsapp"
	NetworkWebsite   SocialNetwork = "website"
)

type Social struct {
	Links []SocialLink `json:"links"`
	Align Align        `json:"align,omitempty"`
}

type SocialLink struct {
	Network SocialNetwork `json:"network"`
	URL     string        `json:"url"`
	Label   string        `json:"label,omitempty"`
}

type Footer struct {
	CompanyName string `json:"company_name"`
	Address     string `json:"address"`
	Text        string `json:"text,omitempty"`
}

func Decode(raw []byte) (Document, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var doc Document
	if err := dec.Decode(&doc); err != nil {
		return Document{}, fmt.Errorf("%w: %w", ErrMalformedDocument, err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Document{}, fmt.Errorf("%w: trailing data after the document", ErrMalformedDocument)
	}
	return doc, nil
}
