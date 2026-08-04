package ticket

// FileStorage is the ticket package's view of the object store.
//
// It mirrors media.FileStorage deliberately: the same S3 service satisfies
// both, so the signatures have to agree or the wiring stops compiling.
type FileStorage interface {
	// UploadFile stores data under key. contentType is the asset's real media
	// type, or "" to let the implementation resolve it from the key's extension
	// and then from the content itself.
	UploadFile(key string, data []byte, contentType string) error
	GetFileURL(key string) string
}
