package voip

import "encoding/binary"

const wavHeaderBytes = 44

func WAV(pcm []byte, sampleRate, channels, bitDepth int) []byte {
	out := make([]byte, wavHeaderBytes, wavHeaderBytes+len(pcm))
	copy(out[0:4], "RIFF")
	binary.LittleEndian.PutUint32(out[4:8], uint32(36+len(pcm)))
	copy(out[8:12], "WAVE")
	copy(out[12:16], "fmt ")
	binary.LittleEndian.PutUint32(out[16:20], 16)
	binary.LittleEndian.PutUint16(out[20:22], 1)
	binary.LittleEndian.PutUint16(out[22:24], uint16(channels))
	binary.LittleEndian.PutUint32(out[24:28], uint32(sampleRate))
	binary.LittleEndian.PutUint32(out[28:32], uint32(sampleRate*channels*bitDepth/8))
	binary.LittleEndian.PutUint16(out[32:34], uint16(channels*bitDepth/8))
	binary.LittleEndian.PutUint16(out[34:36], uint16(bitDepth))
	copy(out[36:40], "data")
	binary.LittleEndian.PutUint32(out[40:44], uint32(len(pcm)))
	return append(out, pcm...)
}
