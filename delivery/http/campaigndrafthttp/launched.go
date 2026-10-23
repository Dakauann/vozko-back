package campaigndrafthttp

import (
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	"vozko/domain/campaign/launchcheck"
	"vozko/usecases/campaignlaunch"
)

type LaunchResponse struct {
	Campaign     any      `json:"campaign"`
	Started      bool     `json:"started"`
	Mode         string   `json:"mode" enums:"now,schedule,ready"`
	Warnings     []string `json:"warnings"`
	StartRefusal string   `json:"startRefusal,omitempty"`
}

type ReviewResponse struct {
	Audience any         `json:"audience"`
	Quote    any         `json:"quote"`
	Estimate any         `json:"estimate"`
	Check    CheckReport `json:"check"`
}

type CheckReport struct {
	Findings          []launchcheck.Finding `json:"findings"`
	Fingerprint       string                `json:"fingerprint"`
	Blocks            bool                  `json:"blocks"`
	NeedsConfirmation bool                  `json:"needsConfirmation"`
}

func CheckOf(r launchcheck.Report) CheckReport {
	findings := r.Findings
	if findings == nil {
		findings = []launchcheck.Finding{}
	}
	return CheckReport{Findings: findings, Fingerprint: r.Fingerprint(), Blocks: r.Blocks(), NeedsConfirmation: r.NeedsConfirmation()}
}

type RefusalDetails struct {
	Blocks      []string              `json:"blocks"`
	Confirms    []string              `json:"confirms"`
	Fingerprint string                `json:"fingerprint"`
	Findings    []launchcheck.Finding `json:"findings"`
}

func codesAt(r launchcheck.Report, level launchcheck.Level) []string {
	codes := []string{}
	for _, f := range r.Findings {
		if f.Level == level {
			codes = append(codes, string(f.Code))
		}
	}
	return codes
}

func RefusalCode(r launchcheck.Report) string {
	if r.Blocks() {
		return "campaign_launch_blocked"
	}
	return "campaign_launch_unconfirmed"
}

func WriteLaunchRefusal(w http.ResponseWriter, err error) bool {
	var refused *launchcheck.RefusedError
	if !errors.As(err, &refused) {
		return false
	}
	report := refused.Report
	response.WriteErrorWithDetails(w, http.StatusConflict, RefusalCode(report), err.Error(), RefusalDetails{
		Blocks: codesAt(report, launchcheck.Block), Confirms: codesAt(report, launchcheck.Confirm), Fingerprint: report.Fingerprint(), Findings: CheckOf(report).Findings,
	})
	return true
}

func WriteLaunched[C any](w http.ResponseWriter, launch campaign.Launch, out campaignlaunch.Outcome[C], dto func(C) any, refusalCode func(error) string) {
	warnings := out.Warnings
	if warnings == nil {
		warnings = []string{}
	}
	body := LaunchResponse{Campaign: dto(out.Campaign), Started: out.Started, Mode: string(launch.Mode), Warnings: warnings}
	if out.StartRefusal != nil {
		body.StartRefusal = startRefusalCode(out.StartRefusal, refusalCode)
	}
	response.WriteSuccess(w, http.StatusCreated, body)
}

func startRefusalCode(err error, channel func(error) string) string {
	var refused *launchcheck.RefusedError
	if errors.As(err, &refused) {
		return RefusalCode(refused.Report)
	}
	if code := campaign.ErrorCode(err); code != "" {
		return code
	}
	if channel != nil {
		if code := channel(err); code != "" {
			return code
		}
	}
	return "campaign_start_failed"
}
