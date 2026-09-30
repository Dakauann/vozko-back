package voip

import (
	"encoding/binary"
	"testing"
)

func TestWAVDescribesEightKilohertzMonoPCM(t *testing.T) {
	pcm := make([]byte, PCMFrameBytes)
	wav := WAV(pcm, 8000, 1, 16)
	if len(wav) != wavHeaderBytes+len(pcm) || string(wav[0:4]) != "RIFF" || string(wav[8:12]) != "WAVE" || string(wav[36:40]) != "data" {
		t.Fatalf("header = %q", wav[:44])
	}
	if rate := binary.LittleEndian.Uint32(wav[24:28]); rate != 8000 {
		t.Fatalf("sample rate = %d", rate)
	}
	if byteRate := binary.LittleEndian.Uint32(wav[28:32]); byteRate != 16000 {
		t.Fatalf("byte rate = %d", byteRate)
	}
	if size := binary.LittleEndian.Uint32(wav[40:44]); size != uint32(len(pcm)) {
		t.Fatalf("data size = %d", size)
	}
}
