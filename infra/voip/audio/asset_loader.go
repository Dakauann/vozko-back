package audio

import (
	"encoding/binary"
	"fmt"
	"io"
	"os"

	"github.com/hajimehoshi/go-mp3"
)

// telephonyRate is the narrowband SIP rate every bridge audio buffer plays at.
const telephonyRate = 8000

// LoadMP3AsTelephonyPCM decodes an MP3 file into 16-bit little-endian MONO PCM
// at 8kHz, the exact format the RTP sender expects, so the result can be
// handed straight to playback with no per-call decode or resample.
//
// It is meant to be called ONCE at startup. The returned buffer is immutable
// and safe to share read-only across any number of concurrent calls, so a
// single decode serves the whole fleet (no per-call I/O, no per-call copies).
func LoadMP3AsTelephonyPCM(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("audio asset: open %q: %w", path, err)
	}
	defer f.Close()

	pcm, err := DecodeMP3AsTelephonyPCM(f)
	if err != nil {
		return nil, fmt.Errorf("audio asset %q: %w", path, err)
	}
	return pcm, nil
}

// DecodeMP3AsTelephonyPCM is the streaming form of LoadMP3AsTelephonyPCM: it
// decodes MP3 bytes from any reader (a downloaded workspace hold music track, an
// uploaded file being validated) into the same immutable 16-bit LE mono 8kHz PCM
// the RTP sender and hold players consume.
func DecodeMP3AsTelephonyPCM(r io.Reader) ([]byte, error) {
	dec, err := mp3.NewDecoder(r)
	if err != nil {
		return nil, fmt.Errorf("audio asset: decode: %w", err)
	}

	// go-mp3 always yields 16-bit little-endian STEREO PCM at the source rate.
	stereo, err := io.ReadAll(dec)
	if err != nil {
		return nil, fmt.Errorf("audio asset: read: %w", err)
	}

	pcm := downmixAndResampleToTelephony(stereo, dec.SampleRate())
	if len(pcm) == 0 {
		return nil, fmt.Errorf("audio asset: decoded to empty PCM")
	}
	return pcm, nil
}

// LoadMP3AsBackgroundBed decodes an MP3 into pre-attenuated mono PCM16 samples
// at 8kHz, ready to be looped as a constant background bed under a call. gain
// scales the amplitude (e.g. ~0.17, about -15 dB, for a soft call-center
// ambience) and is clamped to [0,1]; a gain of 0 yields no bed. Like LoadMP3AsTelephonyPCM it is meant to
// run ONCE at startup, and the returned slice is immutable and safe to share
// read-only across every concurrent call.
func LoadMP3AsBackgroundBed(path string, gain float64) ([]int16, error) {
	pcm, err := LoadMP3AsTelephonyPCM(path)
	if err != nil {
		return nil, err
	}
	if gain <= 0 {
		return nil, nil
	}
	if gain > 1 {
		gain = 1
	}
	n := len(pcm) / 2
	out := make([]int16, n)
	for i := 0; i < n; i++ {
		s := int16(binary.LittleEndian.Uint16(pcm[i*2:]))
		v := float64(s) * gain
		if v > 32767 {
			v = 32767
		} else if v < -32768 {
			v = -32768
		}
		out[i] = int16(v)
	}
	return out, nil
}

// downmixAndResampleToTelephony collapses interleaved 16-bit LE stereo PCM at
// srcRate into mono 16-bit LE PCM at 8kHz. It uses nearest-sample resampling,
// which is adequate for a short ambient asset (a keyboard loop) played over a
// narrowband G.711 line; it is deliberately not the speech-fidelity resampler.
func downmixAndResampleToTelephony(stereo []byte, srcRate int) []byte {
	if srcRate <= 0 {
		srcRate = telephonyRate
	}
	const stereoFrame = 4 // 2 channels * 2 bytes
	srcFrames := len(stereo) / stereoFrame
	if srcFrames == 0 {
		return nil
	}

	dstFrames := srcFrames * telephonyRate / srcRate
	if dstFrames == 0 {
		return nil
	}
	out := make([]byte, dstFrames*2)
	for i := 0; i < dstFrames; i++ {
		off := (i * srcRate / telephonyRate) * stereoFrame
		l := int16(binary.LittleEndian.Uint16(stereo[off:]))
		r := int16(binary.LittleEndian.Uint16(stereo[off+2:]))
		binary.LittleEndian.PutUint16(out[i*2:], uint16(int16((int(l)+int(r))/2)))
	}
	return out
}
