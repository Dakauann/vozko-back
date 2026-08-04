package media

// FileStorage is the object store behind every asset URL the product hands to
// a third party.
type FileStorage interface {
	// UploadFile stores data under key.
	//
	// contentType is REQUIRED to be the asset's real media type whenever the
	// caller knows it, because the stored value becomes the Content-Type the CDN
	// serves, and both Telegram and Meta fetch these URLs themselves and decide
	// from that header whether the asset is sendable at all.
	//
	// It was absent for a long time, so every object was stored with S3's
	// default and served as application/octet-stream. Telegram answered
	// "failed to get HTTP URL content" for every kind of media, and Meta
	// answered "attachment format not accepted" for everything except images,
	// which it recognises by sniffing the bytes.
	//
	// Pass "" only when the type genuinely is not known; the implementation then
	// falls back to the key's extension and finally to sniffing the content.
	UploadFile(key string, data []byte, contentType string) error

	// GetFileURL returns the public URL for key.
	GetFileURL(key string) string
}
