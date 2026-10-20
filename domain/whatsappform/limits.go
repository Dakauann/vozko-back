package whatsappform

const (
	MaxHeadingLength        = 80
	MaxBodyLength           = 4096
	MaxTextInputLabel       = 20
	MaxTextAreaLabel        = 20
	MaxHelpLength           = 80
	MaxChoiceGroupLabel     = 30
	MaxDropdownLabel        = 20
	MaxChipsLabel           = 80
	MaxDateLabel            = 40
	MaxOptInLabel           = 120
	MaxLinkText             = 25
	MaxFooterLabel          = 35
	MaxPickerLabel          = 80
	MaxPickerDescription    = 300
	MaxOptionTitle          = 30
	MaxOptionDescription    = 300
	MaxChoiceGroupOptions   = 20
	MaxDropdownOptions      = 200
	MinChipsOptions         = 2
	MaxChipsOptions         = 20
	DefaultTextInputMaxChar = 80
	DefaultTextAreaMaxChar  = 600
	MaxComponentsPerScreen  = 50
	MaxImagesPerScreen      = 3
	MaxLinksPerScreen       = 2
	MaxOptInsPerScreen      = 5
	MaxPickersPerScreen     = 1
	MaxRouteBranches        = 10
	MaxUploads              = 30
	MaxUploadSizeKB         = 25600
	MaxImageBytes           = 300 * 1024
	MaxKeyLength            = 40
	MaxQuestionLength       = MaxBodyLength
)

var AllowedDocumentMimeTypes = []string{
	"application/gzip", "application/zip", "application/x-7z-compressed",
	"application/pdf", "application/msword", "application/vnd.ms-excel", "application/vnd.ms-powerpoint",
	"application/vnd.openxmlformats-officedocument.wordprocessingml.document",
	"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
	"application/vnd.openxmlformats-officedocument.presentationml.presentation",
	"application/vnd.oasis.opendocument.text", "application/vnd.oasis.opendocument.spreadsheet",
	"application/vnd.oasis.opendocument.presentation",
	"image/avif", "image/gif", "image/heic", "image/heif", "image/jpeg", "image/png", "image/tiff", "image/webp",
	"text/plain", "video/mp4", "video/mpeg",
}

func LabelLimit(kind FieldKind) int {
	switch {
	case kind.IsTextInput():
		return MaxTextInputLabel
	case kind == FieldLongText:
		return MaxTextAreaLabel
	case kind == FieldRadio || kind == FieldCheckbox:
		return MaxChoiceGroupLabel
	case kind == FieldDropdown:
		return MaxDropdownLabel
	case kind == FieldMultiChips:
		return MaxChipsLabel
	case kind == FieldDate || kind == FieldDateRange:
		return MaxDateLabel
	case kind == FieldOptIn:
		return MaxOptInLabel
	case kind.IsMedia():
		return MaxPickerLabel
	}
	return 0
}

func OptionBounds(kind FieldKind) (int, int) {
	switch kind {
	case FieldRadio, FieldCheckbox:
		return 1, MaxChoiceGroupOptions
	case FieldDropdown:
		return 1, MaxDropdownOptions
	case FieldMultiChips:
		return MinChipsOptions, MaxChipsOptions
	}
	return 0, 0
}

func DisplayTextLimit(kind DisplayKind) int {
	switch kind {
	case DisplayHeading, DisplaySubheading:
		return MaxHeadingLength
	case DisplayParagraph, DisplayCaption:
		return MaxBodyLength
	case DisplayLink:
		return MaxLinkText
	}
	return 0
}

type Limits struct {
	Labels              map[FieldKind]int    `json:"labels"`
	Options             map[FieldKind][2]int `json:"options"`
	DisplayText         map[DisplayKind]int  `json:"displayText"`
	Help                int                  `json:"help"`
	OptionTitle         int                  `json:"optionTitle"`
	OptionDescription   int                  `json:"optionDescription"`
	SubmitLabel         int                  `json:"submitLabel"`
	ComponentsPerScreen int                  `json:"componentsPerScreen"`
	ImagesPerScreen     int                  `json:"imagesPerScreen"`
	LinksPerScreen      int                  `json:"linksPerScreen"`
	OptInsPerScreen     int                  `json:"optInsPerScreen"`
	PickersPerScreen    int                  `json:"pickersPerScreen"`
	RouteBranches       int                  `json:"routeBranches"`
	Uploads             int                  `json:"uploads"`
	UploadSizeKB        int                  `json:"uploadSizeKb"`
	ImageBytes          int                  `json:"imageBytes"`
	TextInputMaxChars   int                  `json:"textInputMaxChars"`
	TextAreaMaxChars    int                  `json:"textAreaMaxChars"`
	DocumentMimeTypes   []string             `json:"documentMimeTypes"`
}

func CurrentLimits() Limits {
	labels := map[FieldKind]int{}
	options := map[FieldKind][2]int{}
	for _, kind := range FieldKinds {
		labels[kind] = LabelLimit(kind)
		if kind.HasOptions() {
			lo, hi := OptionBounds(kind)
			options[kind] = [2]int{lo, hi}
		}
	}
	display := map[DisplayKind]int{}
	for _, kind := range DisplayKinds {
		display[kind] = DisplayTextLimit(kind)
	}
	return Limits{
		Labels: labels, Options: options, DisplayText: display,
		Help: MaxHelpLength, OptionTitle: MaxOptionTitle, OptionDescription: MaxOptionDescription,
		SubmitLabel: MaxFooterLabel, ComponentsPerScreen: MaxComponentsPerScreen,
		ImagesPerScreen: MaxImagesPerScreen, LinksPerScreen: MaxLinksPerScreen, OptInsPerScreen: MaxOptInsPerScreen,
		PickersPerScreen: MaxPickersPerScreen, RouteBranches: MaxRouteBranches, Uploads: MaxUploads,
		UploadSizeKB: MaxUploadSizeKB, ImageBytes: MaxImageBytes,
		TextInputMaxChars: DefaultTextInputMaxChar, TextAreaMaxChars: DefaultTextAreaMaxChar,
		DocumentMimeTypes: append([]string(nil), AllowedDocumentMimeTypes...),
	}
}
