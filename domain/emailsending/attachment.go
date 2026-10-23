package emailsending

import (
	"errors"
	"fmt"
	"mime"
	"path"
	"strings"
	"unicode/utf8"
)

const (
	MaxDeliverableMessageBytes int64 = 25_000_000
	MaxAttachmentBytes         int64 = 16 << 20
	OutboundChunkBytes               = InboundChunkBytes
	base64LineLength           int64 = 76
	defaultAttachmentBaseName        = "anexo"
)

var (
	ErrAttachmentBlocked  = errors.New("emailsending: the provider refuses this attachment type")
	ErrAttachmentTooLarge = errors.New("emailsending: the attachments exceed what one email can carry")
	ErrAttachmentInvalid  = errors.New("emailsending: the attachment lacks a valid name, type or content")
	ErrMessageTooLarge    = errors.New("emailsending: the encoded message is over the deliverable size")
)

type Attachment struct {
	FileName    string
	ContentType string
	Data        []byte
}

var blockedAttachmentExtensions = map[string]struct{}{
	".ade": {}, ".adp": {}, ".app": {}, ".asp": {}, ".bas": {}, ".bat": {}, ".cer": {}, ".chm": {}, ".cmd": {},
	".com": {}, ".cpl": {}, ".crt": {}, ".csh": {}, ".der": {}, ".exe": {}, ".fxp": {}, ".gadget": {}, ".hlp": {},
	".hta": {}, ".inf": {}, ".ins": {}, ".isp": {}, ".its": {}, ".js": {}, ".jse": {}, ".ksh": {}, ".lib": {},
	".lnk": {}, ".mad": {}, ".maf": {}, ".mag": {}, ".mam": {}, ".maq": {}, ".mar": {}, ".mas": {}, ".mat": {},
	".mau": {}, ".mav": {}, ".maw": {}, ".mda": {}, ".mdb": {}, ".mde": {}, ".mdt": {}, ".mdw": {}, ".mdz": {},
	".msc": {}, ".msh": {}, ".msh1": {}, ".msh2": {}, ".mshxml": {}, ".msh1xml": {}, ".msh2xml": {}, ".msi": {},
	".msp": {}, ".mst": {}, ".ops": {}, ".pcd": {}, ".pif": {}, ".plg": {}, ".prf": {}, ".prg": {}, ".reg": {},
	".scf": {}, ".scr": {}, ".sct": {}, ".shb": {}, ".shs": {}, ".sys": {}, ".ps1": {}, ".ps1xml": {}, ".ps2": {},
	".ps2xml": {}, ".psc1": {}, ".psc2": {}, ".tmp": {}, ".url": {}, ".vb": {}, ".vbe": {}, ".vbs": {}, ".vps": {},
	".vsmacros": {}, ".vss": {}, ".vst": {}, ".vsw": {}, ".vxd": {}, ".ws": {}, ".wsc": {}, ".wsf": {}, ".wsh": {},
	".xnk": {},
}

var preferredExtensions = map[string]string{
	"image/jpeg":      ".jpg",
	"image/png":       ".png",
	"image/gif":       ".gif",
	"image/webp":      ".webp",
	"application/pdf": ".pdf",
	"audio/mpeg":      ".mp3",
	"audio/ogg":       ".ogg",
	"audio/mp4":       ".m4a",
	"audio/webm":      ".webm",
	"video/mp4":       ".mp4",
	"video/webm":      ".webm",
	"video/quicktime": ".mov",
	"text/plain":      ".txt",
}

func BlockedAttachment(fileName string) bool {
	_, blocked := blockedAttachmentExtensions[strings.ToLower(path.Ext(strings.TrimRight(fileName, ". \t")))]
	return blocked
}

func AttachmentName(fileName, contentType string) string {
	base := strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, path.Base(strings.ReplaceAll(strings.TrimSpace(fileName), `\`, "/")))
	base = strings.TrimSpace(base)
	if base == "" || base == "." || base == "/" {
		base = defaultAttachmentBaseName
	}
	if path.Ext(base) != "" {
		return base
	}
	return base + extensionOf(contentType)
}

func extensionOf(contentType string) string {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return ""
	}
	if ext, ok := preferredExtensions[mediaType]; ok {
		return ext
	}
	if exts, err := mime.ExtensionsByType(mediaType); err == nil && len(exts) > 0 {
		return exts[0]
	}
	return ""
}

func EncodedAttachmentBytes(rawBytes int64) int64 {
	if rawBytes <= 0 {
		return 0
	}
	encoded := (rawBytes + 2) / 3 * 4
	lines := (encoded + base64LineLength - 1) / base64LineLength
	return encoded + 2*lines
}

func EncodedAttachmentsBytes(attachments []Attachment) int64 {
	var total int64
	for _, a := range attachments {
		total += EncodedAttachmentBytes(int64(len(a.Data)))
	}
	return total
}

func AttachmentChunks(encodedBytes int64) int64 {
	if encodedBytes <= 0 {
		return 0
	}
	return (encodedBytes + OutboundChunkBytes - 1) / OutboundChunkBytes
}

func CheckAttachments(attachments []Attachment) error {
	var total int64
	for _, a := range attachments {
		name := strings.TrimSpace(a.FileName)
		if name == "" || strings.ContainsAny(name, "\r\n\x00") || !utf8.ValidString(name) || len(a.Data) == 0 {
			return fmt.Errorf("%w: %q", ErrAttachmentInvalid, a.FileName)
		}
		if mediaType, _, err := mime.ParseMediaType(a.ContentType); err != nil || !strings.Contains(mediaType, "/") {
			return fmt.Errorf("%w: type %q of %q", ErrAttachmentInvalid, a.ContentType, name)
		}
		if BlockedAttachment(name) {
			return fmt.Errorf("%w: %q", ErrAttachmentBlocked, name)
		}
		total += int64(len(a.Data))
	}
	if total > MaxAttachmentBytes {
		return fmt.Errorf("%w: %d bytes over the %d byte budget", ErrAttachmentTooLarge, total, MaxAttachmentBytes)
	}
	return nil
}
