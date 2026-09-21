package media

type FileStorage interface {
	UploadFile(key string, data []byte, contentType string) error

	GetFileURL(key string) string
}
