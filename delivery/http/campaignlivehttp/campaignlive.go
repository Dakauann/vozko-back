package campaignlivehttp

import (
	"context"
	"errors"
	"log"
	"net/http"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	"vozko/usecases/campaignlive"
)

var refusals = []response.ErrorRefusal{
	{Err: campaign.ErrTimelineBucketInvalid, Refusal: response.Refusal{Status: http.StatusBadRequest, Code: "campaign_timeline_bucket_invalid", Message: "Escolha minuto, hora ou dia."}},
	{Err: campaign.ErrTimelineLocationMissing, Refusal: response.Refusal{Status: http.StatusServiceUnavailable, Code: "campaign_live_unavailable", Message: "O fuso do workspace não pôde ser lido agora."}},
	{Err: campaignlive.ErrUnavailable, Refusal: response.Refusal{Status: http.StatusServiceUnavailable, Code: "campaign_live_unavailable", Message: "O andamento da campanha não está disponível agora."}},
	{Err: campaignlive.ErrCampaignMissing, Refusal: response.Refusal{Status: http.StatusNotFound, Code: "campaign_live_unavailable", Message: "Campanha não encontrada."}},
}

var unavailable = response.Refusal{Status: http.StatusServiceUnavailable, Code: "campaign_live_unavailable", Message: "O andamento da campanha não está disponível agora."}

func RefusalCodes() []string {
	return response.RefusalCodes(response.ErrorRefusalsOf(refusals))
}

func WriteProgress(w http.ResponseWriter, r *http.Request, progress campaign.ProgressUseCase, live campaign.LiveCampaign) {
	if progress == nil {
		response.WriteRefusal(w, unavailable)
		return
	}
	out, err := progress.Execute(r.Context(), live)
	if err != nil {
		writeError(w, err, "campaign progress")
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func WriteTimeline(w http.ResponseWriter, r *http.Request, timeline campaign.TimelineUseCase, live campaign.LiveCampaign) {
	if timeline == nil {
		response.WriteRefusal(w, unavailable)
		return
	}
	bucket := campaign.TimelineBucket(strings.TrimSpace(r.URL.Query().Get("bucket")))
	out, err := timeline.Execute(r.Context(), live, bucket)
	if err != nil {
		writeError(w, err, "campaign timeline")
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func writeError(w http.ResponseWriter, err error, subject string) {
	if errors.Is(err, context.Canceled) {
		return
	}
	if httpx.WriteAnalyticsLimit(w, err, subject) || response.WriteErrorRefusal(w, err, refusals) {
		return
	}
	log.Printf("[%s] %v", subject, err)
	response.WriteRefusal(w, unavailable)
}
