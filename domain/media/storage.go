package media

type FileStorage interface {
	UploadFile(key string, data []byte) error
	GetFileURL(key string) string
}
