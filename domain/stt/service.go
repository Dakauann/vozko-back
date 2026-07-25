package stt

import "context"

type STTService interface {
	Transcribe(ctx context.Context, audioData []byte, language string) (*Transcription, error)
}

type STTTranscriber interface {
	TranscribeWithPreviousText(ctx context.Context, audioData []byte, language, previousText string) (*Transcription, error)
}
