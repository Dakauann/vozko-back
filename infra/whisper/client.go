package whisper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"mime/multipart"
	"net/http"
	"strings"
	"time"
	"unicode"

	"vozko/domain/stt"
)

type ServerType string

const (
	ServerTypeWhisperCpp ServerType = "whisper_cpp"
	ServerTypeSpeaches   ServerType = "speaches"
)

// pcmSampleRate is the 16 kHz mono rate the transcription servers expect; it is
// used to estimate an audio duration from a raw PCM byte count.
const pcmSampleRate = 16000

type Client struct {
	baseURL       string
	httpClient    *http.Client
	model         string
	serverType    ServerType
	language      string
	filterConfig  stt.FilterConfig
	initialPrompt string
}

type Config struct {
	BaseURL       string
	Model         string
	ServerType    ServerType
	Language      string
	Timeout       time.Duration
	FilterConfig  *stt.FilterConfig
	InitialPrompt string
}

func NewClient(cfg Config) *Client {
	if cfg.BaseURL == "" {
		cfg.BaseURL = "http://localhost:17071"
	}
	if cfg.Model == "" {
		cfg.Model = "ggml-large-v3-turbo"
	}
	if cfg.Language == "" {
		cfg.Language = "pt"
	}
	if cfg.Timeout <= 0 {
		cfg.Timeout = 30 * time.Second
	}
	if cfg.ServerType == "" {

		cfg.ServerType = ServerTypeWhisperCpp
	}

	filterCfg := stt.DefaultFilterConfig()
	if cfg.FilterConfig != nil {
		filterCfg = *cfg.FilterConfig
	}

	initialPrompt := cfg.InitialPrompt
	if initialPrompt == "" {
		initialPrompt = "Alô, bom dia. Quais informações você precisa? Tudo bem, pode falar. Certo, entendi. Obrigado, até logo."
	}

	return &Client{
		baseURL:       strings.TrimSuffix(cfg.BaseURL, "/"),
		httpClient:    &http.Client{Timeout: cfg.Timeout},
		model:         cfg.Model,
		serverType:    cfg.ServerType,
		language:      cfg.Language,
		filterConfig:  filterCfg,
		initialPrompt: initialPrompt,
	}
}

var _ stt.STTService = (*Client)(nil)

type verboseResponse struct {
	Text     string           `json:"text"`
	Language string           `json:"language"`
	Duration float64          `json:"duration"`
	Segments []verboseSegment `json:"segments"`
}

type verboseSegment struct {
	ID               int     `json:"id"`
	Start            float64 `json:"start"`
	End              float64 `json:"end"`
	Text             string  `json:"text"`
	AvgLogprob       float64 `json:"avg_logprob"`
	NoSpeechProb     float64 `json:"no_speech_prob"`
	CompressionRatio float64 `json:"compression_ratio"`
}

type whisperCppResponse struct {
	Text string `json:"text"`
}

func (c *Client) Transcribe(ctx context.Context, audioData []byte, language string) (*stt.Transcription, error) {
	switch c.serverType {
	case ServerTypeWhisperCpp:
		return c.transcribeWhisperCpp(ctx, audioData, language)
	default:
		return c.transcribeSpeaches(ctx, audioData, language)
	}
}

func (c *Client) transcribeWhisperCpp(ctx context.Context, audioData []byte, language string) (*stt.Transcription, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	filename := "audio.wav"
	isCompressedFormat := false
	if len(audioData) >= 4 {
		if string(audioData[:4]) == "OggS" {
			filename = "audio.ogg"
			isCompressedFormat = true
		} else if audioData[0] == 0x1A && audioData[1] == 0x45 && audioData[2] == 0xDF && audioData[3] == 0xA3 {
			filename = "audio.webm"
			isCompressedFormat = true
		} else if string(audioData[:4]) == "fLaC" {
			filename = "audio.flac"
			isCompressedFormat = true
		} else if len(audioData) >= 12 && string(audioData[8:12]) == "WAVE" {
			filename = "audio.wav"
		} else if (audioData[0] == 0xFF && (audioData[1]&0xE0) == 0xE0) || string(audioData[:3]) == "ID3" {
			filename = "audio.mp3"
			isCompressedFormat = true
		}
	}

	part, err := writer.CreateFormFile("file", filename)
	if err != nil {
		return nil, fmt.Errorf("whisper.cpp: create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, fmt.Errorf("whisper.cpp: write audio data: %w", err)
	}

	if err := writer.WriteField("response_format", "json"); err != nil {
		return nil, fmt.Errorf("whisper.cpp: write response_format: %w", err)
	}

	lang := c.language
	if language != "" {
		lang = language
	}
	if lang != "" {
		if err := writer.WriteField("language", lang); err != nil {
			return nil, fmt.Errorf("whisper.cpp: write language: %w", err)
		}
	}

	if err := writer.WriteField("temperature", "0.0"); err != nil {
		return nil, fmt.Errorf("whisper.cpp: write temperature: %w", err)
	}
	if err := writer.WriteField("temperature_inc", "0.2"); err != nil {
		return nil, fmt.Errorf("whisper.cpp: write temperature_inc: %w", err)
	}

	if c.initialPrompt != "" {
		if err := writer.WriteField("prompt", c.initialPrompt); err != nil {
			return nil, fmt.Errorf("whisper.cpp: write prompt: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("whisper.cpp: close writer: %w", err)
	}

	url := c.baseURL + "/inference"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, fmt.Errorf("whisper.cpp: create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whisper.cpp: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("whisper.cpp: server returned %d: %s", resp.StatusCode, string(body))
	}

	var cppResp whisperCppResponse
	if err := json.NewDecoder(resp.Body).Decode(&cppResp); err != nil {
		return nil, fmt.Errorf("whisper.cpp: decode response: %w", err)
	}

	text := strings.TrimSpace(cppResp.Text)

	var estimatedDuration float64
	if isCompressedFormat {
		estimatedDuration = 0
	} else {

		estimatedDuration = float64(len(audioData)) / float64(pcmSampleRate) / 2
	}

	result := &stt.Transcription{
		Text:       text,
		Language:   language,
		Confidence: estimateConfidence(text),
		Duration:   estimatedDuration,
	}

	if !c.shouldAccept(result, 0.0) {
		return &stt.Transcription{}, nil
	}

	return result, nil
}

func (c *Client) transcribeSpeaches(ctx context.Context, audioData []byte, language string) (*stt.Transcription, error) {
	var buf bytes.Buffer
	writer := multipart.NewWriter(&buf)

	if err := writer.WriteField("model", c.model); err != nil {
		return nil, fmt.Errorf("whisper: write model field: %w", err)
	}

	if err := writer.WriteField("response_format", "verbose_json"); err != nil {
		return nil, fmt.Errorf("whisper: write response_format field: %w", err)
	}

	part, err := writer.CreateFormFile("file", "audio.wav")
	if err != nil {
		return nil, fmt.Errorf("whisper: create form file: %w", err)
	}
	if _, err := part.Write(audioData); err != nil {
		return nil, fmt.Errorf("whisper: write audio data: %w", err)
	}

	if language != "" {
		lang := mapLanguageCode(language)
		if err := writer.WriteField("language", lang); err != nil {
			return nil, fmt.Errorf("whisper: write language field: %w", err)
		}
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("whisper: close writer: %w", err)
	}

	url := c.baseURL + "/v1/audio/transcriptions"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, &buf)
	if err != nil {
		return nil, fmt.Errorf("whisper: create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("whisper: request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("whisper: server returned %d: %s", resp.StatusCode, string(body))
	}

	var verbose verboseResponse
	if err := json.NewDecoder(resp.Body).Decode(&verbose); err != nil {
		return nil, fmt.Errorf("whisper: decode response: %w", err)
	}

	result := &stt.Transcription{
		Text:     strings.TrimSpace(verbose.Text),
		Language: verbose.Language,
		Duration: verbose.Duration,
	}

	var totalConfidence float64
	var totalNoSpeech float64
	for _, seg := range verbose.Segments {
		result.Segments = append(result.Segments, stt.Segment{
			ID:         seg.ID,
			Start:      seg.Start,
			End:        seg.End,
			Text:       strings.TrimSpace(seg.Text),
			Confidence: logProbToConfidence(seg.AvgLogprob),
			NoSpeech:   seg.NoSpeechProb,
		})
		totalConfidence += logProbToConfidence(seg.AvgLogprob)
		totalNoSpeech += seg.NoSpeechProb
	}

	if len(verbose.Segments) > 0 {
		result.Confidence = totalConfidence / float64(len(verbose.Segments))
	}

	if !c.shouldAccept(result, totalNoSpeech/float64(max(len(verbose.Segments), 1))) {
		return &stt.Transcription{}, nil
	}

	return result, nil
}

func estimateConfidence(text string) float64 {
	if text == "" {
		return 0.0
	}

	words := countWords(text)
	if words == 0 {
		return 0.1
	}

	lowerText := strings.ToLower(text)

	hallucinations := []string{
		"thanks for watching",
		"subscribe",
		"like and subscribe",
		"[música]",
		"[music]",
		"...",
	}

	for _, h := range hallucinations {
		if strings.Contains(lowerText, h) {
			return 0.2
		}
	}

	if hasRepetitivePattern(lowerText) {
		return 0.15
	}

	if words >= 3 && words <= 50 {
		return 0.85
	}
	if words >= 1 && words < 3 {
		return 0.7
	}

	return 0.6
}

func (c *Client) shouldAccept(t *stt.Transcription, avgNoSpeech float64) bool {
	text := strings.TrimSpace(t.Text)

	if text == "" {
		return false
	}

	if t.Duration > 0 && t.Duration < c.filterConfig.MinDuration {
		return false
	}

	if t.Confidence < c.filterConfig.MinConfidence {
		return false
	}

	if avgNoSpeech > c.filterConfig.MaxNoSpeechProb {
		return false
	}

	words := countWords(text)
	if words < c.filterConfig.MinWordCount {
		return false
	}

	return true
}

func countWords(text string) int {
	words := strings.Fields(text)
	count := 0
	for _, w := range words {

		hasLetterOrDigit := false
		for _, r := range w {
			if unicode.IsLetter(r) || unicode.IsDigit(r) {
				hasLetterOrDigit = true
				break
			}
		}
		if hasLetterOrDigit {
			count++
		}
	}
	return count
}

func logProbToConfidence(logProb float64) float64 {

	if logProb >= 0 {
		return 1.0
	}

	confidence := math.Exp(logProb)
	if confidence < 0.05 {
		confidence = 0.05
	}
	return confidence
}

func mapLanguageCode(lang string) string {
	switch strings.ToLower(lang) {
	case "pt-br", "pt_br", "portuguese":
		return "pt"
	case "en-us", "en_us", "english":
		return "en"
	case "es-es", "es_es", "spanish":
		return "es"
	default:
		if len(lang) >= 2 {
			return strings.ToLower(lang[:2])
		}
		return lang
	}
}

func hasRepetitivePattern(text string) bool {
	words := strings.Fields(strings.ToLower(text))
	if len(words) < 4 {
		return false
	}

	consecutiveRepeats := 1
	maxConsecutive := 1
	for i := 1; i < len(words); i++ {
		if words[i] == words[i-1] {
			consecutiveRepeats++
			if consecutiveRepeats > maxConsecutive {
				maxConsecutive = consecutiveRepeats
			}
		} else {
			consecutiveRepeats = 1
		}
	}

	if maxConsecutive >= 3 {
		return true
	}

	if len(words) >= 6 {
		for i := 0; i < len(words)-5; i++ {
			pattern := words[i] + " " + words[i+1]
			repeatCount := 1
			for j := i + 2; j < len(words)-1; j += 2 {
				nextPair := words[j] + " " + words[j+1]
				if nextPair == pattern {
					repeatCount++
				} else {
					break
				}
			}
			if repeatCount >= 3 {
				return true
			}
		}
	}

	wordCounts := make(map[string]int)
	for _, w := range words {
		if len(w) > 1 {
			wordCounts[w]++
		}
	}

	threshold := float64(len(words)) * 0.4
	for _, count := range wordCounts {
		if float64(count) >= threshold && count >= 4 {
			return true
		}
	}

	return false
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
