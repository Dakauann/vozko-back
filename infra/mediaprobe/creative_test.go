package mediaprobe

import (
	"bytes"
	"encoding/binary"
	"io"
	"testing"

	ads "vozko/domain/advertising"
)

func box(kind string, payload ...[]byte) []byte {
	body := bytes.Join(payload, nil)
	out := make([]byte, 8, 8+len(body))
	binary.BigEndian.PutUint32(out, uint32(8+len(body)))
	copy(out[4:], kind)
	return append(out, body...)
}

func u32(values ...uint32) []byte {
	out := make([]byte, 4*len(values))
	for i, v := range values {
		binary.BigEndian.PutUint32(out[4*i:], v)
	}
	return out
}

func u64(v uint64) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint64(out, v)
	return out
}

func ftyp(brand string) []byte {
	return box("ftyp", []byte(brand), u32(0), []byte("isom"))
}

func mvhd(timescale, duration uint32) []byte {
	return box("mvhd", u32(0, 0, 0, timescale, duration), make([]byte, 80))
}

func mvhdV1(timescale uint32, duration uint64) []byte {
	return box("mvhd", u32(1<<24), u64(0), u64(0), u32(timescale), u64(duration), make([]byte, 80))
}

var identity = []uint32{1 << 16, 0, 0, 0, 1 << 16, 0, 0, 0, 1 << 30}

var quarterTurn = []uint32{0, 1 << 16, 0, 0xFFFF0000, 0, 0, 0, 0, 1 << 30}

func tkhd(width, height uint32, matrix []uint32) []byte {
	return box("tkhd", u32(0, 0, 0, 1, 0, 0), u32(0, 0), u32(0, 0), u32(matrix...), u32(width<<16, height<<16))
}

func movie(brand string, header []byte, tracks ...[]byte) []byte {
	traks := make([][]byte, 0, len(tracks))
	for _, track := range tracks {
		traks = append(traks, box("trak", track))
	}
	return bytes.Join([][]byte{ftyp(brand), box("mdat", make([]byte, 64)), box("moov", append([][]byte{header}, traks...)...)}, nil)
}

func TestImageFactsComeFromTheFileHeader(t *testing.T) {
	probe := NewCreative()
	data := encodedPNG(t, 1080, 1350)
	facts, err := probe.Facts(ads.MediaImage, whole(data))
	if err != nil {
		t.Fatal(err)
	}
	want := ads.MediaFacts{Kind: ads.MediaImage, Container: ads.ContainerPNG, Bytes: int64(len(data)), Width: 1080, Height: 1350}
	if facts != want {
		t.Fatalf("facts %+v, want %+v", facts, want)
	}
	jpg, err := probe.Facts(ads.MediaImage, whole(encodedJPEG(t, 640, 640)))
	if err != nil || jpg.Container != ads.ContainerJPEG || jpg.Width != 640 {
		t.Fatalf("jpeg facts %+v err %v", jpg, err)
	}
}

func TestVideoFactsComeFromTheMovieBoxEvenAfterTheMediaData(t *testing.T) {
	data := movie("isom", mvhd(1000, 15_000), tkhd(0, 0, identity), tkhd(1080, 1920, identity))
	facts, err := NewCreative().Facts(ads.MediaVideo, whole(data))
	if err != nil {
		t.Fatal(err)
	}
	want := ads.MediaFacts{Kind: ads.MediaVideo, Container: ads.ContainerMP4, Bytes: int64(len(data)), Width: 1080, Height: 1920, DurationMS: 15_000}
	if facts != want {
		t.Fatalf("facts %+v, want %+v", facts, want)
	}
}

func TestARotatedTrackReportsTheShapeItPlaysIn(t *testing.T) {
	facts, err := NewCreative().Facts(ads.MediaVideo, whole(movie("qt  ", mvhdV1(600, 6_000), tkhd(1920, 1080, quarterTurn))))
	if err != nil {
		t.Fatal(err)
	}
	if facts.Container != ads.ContainerMOV || facts.Width != 1080 || facts.Height != 1920 || facts.DurationMS != 10_000 {
		t.Fatalf("facts %+v", facts)
	}
}

func TestUnreadableFilesAreRefused(t *testing.T) {
	probe := NewCreative()
	for name, c := range map[string]struct {
		kind ads.MediaKind
		data []byte
	}{
		"garbage image":      {ads.MediaImage, []byte("not an image")},
		"movie without moov": {ads.MediaVideo, bytes.Join([][]byte{ftyp("isom"), box("mdat", make([]byte, 8))}, nil)},
		"truncated box":      {ads.MediaVideo, append(ftyp("isom"), u32(4096)...)},
		"no picture track":   {ads.MediaVideo, movie("isom", mvhd(1000, 5_000), tkhd(0, 0, identity))},
		"zero timescale":     {ads.MediaVideo, movie("isom", mvhd(0, 5_000), tkhd(640, 640, identity))},
	} {
		if _, err := probe.Facts(c.kind, whole(c.data)); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestANonISOVideoNamesItsContainerSoTheSpecCanRefuseIt(t *testing.T) {
	webm := append([]byte{0x1A, 0x45, 0xDF, 0xA3}, make([]byte, 64)...)
	facts, err := NewCreative().Facts(ads.MediaVideo, whole(webm))
	if err != nil {
		t.Fatal(err)
	}
	if facts.Container != "webm" || facts.Issue() != ads.CodeMediaFormat {
		t.Fatalf("facts %+v", facts)
	}
	other, err := NewCreative().Facts(ads.MediaVideo, whole([]byte("not a movie at all")))
	if err != nil || other.Issue() != ads.CodeMediaFormat {
		t.Fatalf("an unknown container must be refused by the spec, got %+v %v", other, err)
	}
}

type memoryFile struct {
	*bytes.Reader
}

func (f memoryFile) Size() int64 { return f.Reader.Size() }

func whole(data []byte) ads.MediaFile {
	return memoryFile{bytes.NewReader(data)}
}

type sparseMovie struct {
	head, tail []byte
	gap        int64
	read       int64
}

func (s *sparseMovie) Size() int64 { return int64(len(s.head)) + s.gap + int64(len(s.tail)) }

func (s *sparseMovie) ReadAt(p []byte, off int64) (int, error) {
	n := 0
	for n < len(p) && off+int64(n) < s.Size() {
		at := off + int64(n)
		switch {
		case at < int64(len(s.head)):
			p[n] = s.head[at]
		case at < int64(len(s.head))+s.gap:
			p[n] = 0
		default:
			p[n] = s.tail[at-int64(len(s.head))-s.gap]
		}
		n++
	}
	s.read += int64(n)
	if n < len(p) {
		return n, io.EOF
	}
	return n, nil
}

func TestAVideoIsProbedFromItsBoxHeadersAndMovieBoxOnly(t *testing.T) {
	const gap = int64(400 << 20)
	mdatHeader := make([]byte, 16)
	binary.BigEndian.PutUint32(mdatHeader, 1)
	copy(mdatHeader[4:], "mdat")
	binary.BigEndian.PutUint64(mdatHeader[8:], uint64(gap)+16)
	head := append(ftyp("isom"), mdatHeader...)
	tail := box("moov", mvhd(1000, 600_000), box("trak", tkhd(1080, 1920, identity)))
	file := &sparseMovie{head: head, tail: tail, gap: gap}
	facts, err := NewCreative().Facts(ads.MediaVideo, file)
	if err != nil {
		t.Fatal(err)
	}
	if facts.Bytes != file.Size() || facts.DurationMS != 600_000 || facts.Width != 1080 || facts.Height != 1920 {
		t.Fatalf("facts %+v", facts)
	}
	if file.read > 4096 {
		t.Fatalf("read %d bytes of a %d byte file", file.read, file.Size())
	}
}

func TestAnOversizedMovieBoxIsRefused(t *testing.T) {
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header, uint32(maxMovieBoxBytes+9))
	copy(header[4:], "moov")
	file := &sparseMovie{head: append(ftyp("isom"), header...), gap: maxMovieBoxBytes + 1}
	if _, err := NewCreative().Facts(ads.MediaVideo, file); err == nil {
		t.Fatal("a movie box past the read limit was accepted")
	}
}
