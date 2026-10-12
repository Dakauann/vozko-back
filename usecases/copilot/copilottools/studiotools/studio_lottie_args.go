package studiotools

import "strings"

func (o studioVideoOperation) lottieIssue() string {
	if o.Op != "import_lottie" {
		return ""
	}
	if strings.TrimSpace(o.MediaID) == "" {
		return "import_lottie precisa de media_id de um arquivo Lottie da biblioteca (mídia de tipo lottie em studio_read com detail full)"
	}
	return ""
}
