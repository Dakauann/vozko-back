package stt

type Transcription struct {
	Text       string    `json:"text"`
	Language   string    `json:"language,omitempty"`
	Duration   float64   `json:"duration,omitempty"`
	Segments   []Segment `json:"segments,omitempty"`
	Confidence float64   `json:"confidence,omitempty"`
}

type Segment struct {
	ID         int     `json:"id"`
	Start      float64 `json:"start"`
	End        float64 `json:"end"`
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence,omitempty"`
	NoSpeech   float64 `json:"no_speech_prob,omitempty"`
}

type FilterConfig struct {
	MinConfidence   float64
	MinWordCount    int
	MaxNoSpeechProb float64
	MinDuration     float64
	RejectPatterns  []string
}

func DefaultFilterConfig() FilterConfig {
	return FilterConfig{
		MinConfidence:   0.10,
		MinWordCount:    1,
		MaxNoSpeechProb: 0.85,
		MinDuration:     0.30,
	}
}
