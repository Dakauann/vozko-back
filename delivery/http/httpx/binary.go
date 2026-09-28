package httpx

import (
	"net/http"
	"strconv"
	"time"
)

func WriteBinary(w http.ResponseWriter, data []byte, contentType, fallbackType string, maxAge time.Duration) {
	if contentType == "" {
		contentType = fallbackType
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Header().Set("Cache-Control", "private, max-age="+strconv.Itoa(int(maxAge.Seconds())))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(data)
}
