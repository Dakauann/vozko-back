package smartcampaignhttp

import (
	"time"

	"vozko/domain/whatsapp_campaign/smartcampaign"
	"vozko/domain/working_hours"
)

type PolicyBody struct {
	Preset                string              `json:"preset" example:"standard" enums:"conservative,standard,aggressive"`
	FirstWaveSize         int                 `json:"firstWaveSize" example:"200"`
	MinWaveSize           int                 `json:"minWaveSize" example:"50"`
	MaxWaveSize           int                 `json:"maxWaveSize" example:"5000"`
	Growth                float64             `json:"growth" example:"2"`
	Shrink                float64             `json:"shrink" example:"0.5"`
	ObserveWaitMinutes    int                 `json:"observeWaitMinutes" example:"120"`
	MaxObserveWaitMinutes int                 `json:"maxObserveWaitMinutes" example:"300"`
	MaxWavesPerDay        int                 `json:"maxWavesPerDay" example:"0"`
	MaxSlowWavesAtFloor   int                 `json:"maxSlowWavesAtFloor" example:"3"`
	MinSample             int                 `json:"minSample" example:"50"`
	FailureRateSlow       float64             `json:"failureRateSlow" example:"0.08"`
	FailureRateStop       float64             `json:"failureRateStop" example:"0.15"`
	EngagementRateSlow    float64             `json:"engagementRateSlow" example:"0.03"`
	OnTemplateYellow      string              `json:"onTemplateYellow" example:"pause" enums:"slow_down,pause,stop"`
	SendHours             *working_hours.Spec `json:"sendHours"`
}

type DefaultsBody struct {
	SendHours *working_hours.Spec   `json:"sendHours"`
	Presets   map[string]PolicyBody `json:"presets"`
}

type Response struct {
	Policy   *PolicyBody            `json:"policy"`
	Editable bool                   `json:"editable"`
	Defaults DefaultsBody           `json:"defaults"`
	Progress smartcampaign.Progress `json:"progress"`
	Waves    []smartcampaign.Wave   `json:"waves"`
}

func (b PolicyBody) ToDomain() (smartcampaign.Policy, error) {
	preset, err := smartcampaign.ParsePreset(b.Preset)
	if err != nil {
		return smartcampaign.Policy{}, err
	}
	return smartcampaign.Policy{
		Preset:              preset,
		FirstWaveSize:       b.FirstWaveSize,
		MinWaveSize:         b.MinWaveSize,
		MaxWaveSize:         b.MaxWaveSize,
		Growth:              b.Growth,
		Shrink:              b.Shrink,
		ObserveWait:         time.Duration(b.ObserveWaitMinutes) * time.Minute,
		MaxObserveWait:      time.Duration(b.MaxObserveWaitMinutes) * time.Minute,
		MaxWavesPerDay:      b.MaxWavesPerDay,
		MaxSlowWavesAtFloor: b.MaxSlowWavesAtFloor,
		MinSample:           b.MinSample,
		FailureRateSlow:     b.FailureRateSlow,
		FailureRateStop:     b.FailureRateStop,
		EngagementRateSlow:  b.EngagementRateSlow,
		OnTemplateYellow:    smartcampaign.Action(b.OnTemplateYellow),
		SendHours:           b.SendHours,
	}, nil
}

func policyBody(p smartcampaign.Policy) PolicyBody {
	return PolicyBody{
		Preset:                string(p.Preset),
		FirstWaveSize:         p.FirstWaveSize,
		MinWaveSize:           p.MinWaveSize,
		MaxWaveSize:           p.MaxWaveSize,
		Growth:                p.Growth,
		Shrink:                p.Shrink,
		ObserveWaitMinutes:    int(p.ObserveWait / time.Minute),
		MaxObserveWaitMinutes: int(p.MaxObserveWait / time.Minute),
		MaxWavesPerDay:        p.MaxWavesPerDay,
		MaxSlowWavesAtFloor:   p.MaxSlowWavesAtFloor,
		MinSample:             p.MinSample,
		FailureRateSlow:       p.FailureRateSlow,
		FailureRateStop:       p.FailureRateStop,
		EngagementRateSlow:    p.EngagementRateSlow,
		OnTemplateYellow:      string(p.OnTemplateYellow),
		SendHours:             p.SendHours,
	}
}
