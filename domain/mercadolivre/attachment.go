package mercadolivre

import (
	"fmt"
	"mime"
	"strings"
	"unicode/utf8"
)

const MaxAttachmentBytes int64 = 25 << 20

const MaxAttachmentNameRunes = 200

const MaxAttachmentsPerMessage = 25

const (
	MIMEJPEG = "image/jpeg"
	MIMEPNG  = "image/png"
	MIMEPDF  = "application/pdf"
	MIMEText = "text/plain"
)

func AttachmentMIMETypes() []string { return []string{MIMEJPEG, MIMEPNG, MIMEPDF, MIMEText} }

func ValidateAttachment(fileName, contentType string, size int64) error {
	name := strings.TrimSpace(fileName)
	if name == "" || strings.ContainsAny(name, `/\`) || utf8.RuneCountInString(name) > MaxAttachmentNameRunes {
		return fmt.Errorf("%w: file name %q", ErrMediaUnsupported, fileName)
	}
	if size <= 0 {
		return fmt.Errorf("%w: empty file", ErrMediaUnsupported)
	}
	if size > MaxAttachmentBytes {
		return fmt.Errorf("%w: %d bytes", ErrMediaTooLarge, size)
	}
	base, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return fmt.Errorf("%w: content type %q", ErrMediaUnsupported, contentType)
	}
	for _, allowed := range AttachmentMIMETypes() {
		if base == allowed {
			return nil
		}
	}
	return fmt.Errorf("%w: content type %q", ErrMediaUnsupported, contentType)
}
