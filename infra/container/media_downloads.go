package container

import (
	"net/http"
	"time"

	media_domain "vozko/domain/media"
	"vozko/infra/netguard"
	"vozko/infra/remotefile"
	"vozko/usecases/workflow/node_executors"
)

const mediaDownloadTimeout = 60 * time.Second

func mediaDownloads() media_domain.RemoteFiles {
	return remotefile.NewDownloader(netguard.NewHTTPClient(mediaDownloadTimeout))
}

func workflowMediaFetcher() node_executors.MediaFetcher {
	return node_executors.MediaFetcherOf(mediaDownloads())
}

func formImageClient() *http.Client {
	return netguard.NewHTTPClient(formImageFetchTimeout)
}

func voiceAudioClient() *http.Client {
	return netguard.NewHTTPClient(voiceAudioDownloadTimeout)
}

func renderMediaClient() *http.Client {
	return netguard.NewHTTPClient(renderDownloadTimeout)
}
