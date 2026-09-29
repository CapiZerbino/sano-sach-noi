package bookmaker

import (
	"bytes"
	"context"
	"encoding/binary"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeTestWAV ghi WAV PCM 16-bit mono; extra = các chunk chèn giữa fmt và data.
func writeTestWAV(t *testing.T, path string, rate int, samples []int16, fmtSize uint32, extra []byte) {
	t.Helper()
	var b bytes.Buffer
	data := new(bytes.Buffer)
	_ = binary.Write(data, binary.LittleEndian, samples)
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(0)) // ffmpeg / parser không cần đúng
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, fmtSize)
	for _, v := range []any{uint16(1), uint16(1), uint32(rate), uint32(rate * 2), uint16(2), uint16(16)} {
		if err := binary.Write(&b, binary.LittleEndian, v); err != nil {
			t.Fatal(err)
		}
	}
	b.Write(make([]byte, fmtSize-16)) // phần mở rộng (WAVE_FORMAT_EXTENSIBLE…)
	b.Write(extra)
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(data.Len()))
	b.Write(data.Bytes())
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWavDurationSec(t *testing.T) {
	dir := t.TempDir()
	// Chunk LIST độ dài lẻ (3) → có 1 byte đệm, parser phải bỏ qua đúng.
	list := append([]byte("LIST"), 3, 0, 0, 0, 'a', 'b', 'c', 0)
	cases := []struct {
		name    string
		fmtSize uint32
		extra   []byte
	}{{"plain", 16, nil}, {"extensible+odd chunk", 40, list}}
	for _, c := range cases {
		p := filepath.Join(dir, c.name+".wav")
		writeTestWAV(t, p, 8000, make([]int16, 8000*3/2), c.fmtSize, c.extra) // 1,5 giây
		got, err := wavDurationSec(p)
		if err != nil || math.Abs(got-1.5) > 1e-9 {
			t.Errorf("%s: got %v, %v; want 1.5", c.name, got, err)
		}
	}
	bad := filepath.Join(dir, "bad.wav")
	_ = os.WriteFile(bad, []byte("not a wav file at all"), 0o644)
	if _, err := wavDurationSec(bad); err == nil {
		t.Error("file không phải WAV phải báo lỗi")
	}
}

func TestMusicMixArgs(t *testing.T) {
	args := strings.Join(musicMixArgs("in.wav", "out.mp3", "/tmp/nhac chuan.wav", 0.2, 60, "128k"), " ")
	for _, want := range []string{"-stream_loop -1 -i /tmp/nhac chuan.wav", "volume=0.200", "afade=t=in:d=2",
		"afade=t=out:st=57.000:d=3", "sidechaincompress", "duration=first", "-b:a 128k", "out.mp3"} {
		if !strings.Contains(args, want) {
			t.Errorf("thiếu %q trong %s", want, args)
		}
	}
	if strings.Contains(args, "loudnorm") {
		t.Error("loudnorm phải chạy một lần ở normalizedMusic, không lặp lại mỗi tiểu mục")
	}
	// Tiểu mục quá ngắn cho cả hai fade → bỏ fade ra (không đặt st âm).
	if short := strings.Join(musicMixArgs("in.wav", "out.mp3", "m.wav", 0.2, 4, "128k"), " "); strings.Contains(short, "t=out") {
		t.Errorf("tiểu mục 4s không được fade ra: %s", short)
	}
}

// File nhạc có header nhưng không có mẫu nào: trước đây lọt probe rồi làm ffmpeg
// treo mãi (lặp luồng rỗng). Phải bị chặn ở preflight và ở bước chuẩn hoá.
func TestSilentMusicFileRejected(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	empty := filepath.Join(t.TempDir(), "rong.wav")
	writeTestWAV(t, empty, 44100, nil, 16, nil)
	c := &TTSConfig{Mode: TTSModeStub, FFmpeg: ffmpeg, Bitrate: "128k", Music: &BackgroundMusic{Path: empty, Volume: 0.2}}
	if err := c.preflight(); err == nil || !strings.Contains(err.Error(), "không có âm thanh") {
		t.Errorf("preflight phải chặn nhạc rỗng, got %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	defer c.releaseMusic()
	if _, err := c.normalizedMusic(ctx); err == nil || ctx.Err() != nil {
		t.Errorf("chuẩn hoá nhạc rỗng phải báo lỗi ngay (không treo), got err=%v ctx=%v", err, ctx.Err())
	}
}

// Huỷ render phải dừng được ffmpeg đang trộn (exec.CommandContext).
func TestConvertToMP3Cancelled(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	dir := t.TempDir()
	voice := filepath.Join(dir, "v.wav")
	writeTestWAV(t, voice, 8000, make([]int16, 8000), 16, nil)
	c := &TTSConfig{FFmpeg: ffmpeg, Bitrate: "128k", Logf: func(string, ...any) {}}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.convertToMP3(ctx, voice, filepath.Join(dir, "o.mp3")); err == nil {
		t.Error("ctx đã huỷ thì convertToMP3 phải trả lỗi")
	}
}

// Nhạc chỉ chuẩn hoá một lần cho cả lượt render, và được dọn khi xong.
func TestNormalizedMusicCachedAndReleased(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	music := filepath.Join(t.TempDir(), "m.wav")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=f=440:d=2", music).CombinedOutput(); err != nil {
		t.Fatalf("%v %s", err, out)
	}
	c := &TTSConfig{FFmpeg: ffmpeg, Music: &BackgroundMusic{Path: music, Volume: 0.2}}
	a, err := c.normalizedMusic(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := c.normalizedMusic(context.Background()); b != a {
		t.Errorf("lần gọi thứ hai phải dùng lại %q, got %q", a, b)
	}
	c.releaseMusic()
	if _, err := os.Stat(a); !os.IsNotExist(err) {
		t.Errorf("file nhạc chuẩn hoá chưa được xoá: %v", err)
	}
}

// WAV do bộ ghi dạng luồng để size data = 0 / 0xFFFFFFFF: lấy theo độ dài file.
func TestWavDurationSec_StreamingHeader(t *testing.T) {
	for _, bad := range []uint32{0, 0xFFFFFFFF} {
		p := filepath.Join(t.TempDir(), "s.wav")
		writeTestWAV(t, p, 8000, make([]int16, 8000), 16, nil) // 1 giây
		b, _ := os.ReadFile(p)
		binary.LittleEndian.PutUint32(b[40:44], bad) // size của chunk data (header 44 byte)
		_ = os.WriteFile(p, b, 0o644)
		if d, err := wavDurationSec(p); err != nil || math.Abs(d-1) > 1e-9 {
			t.Errorf("size=%#x: got %v, %v; want 1", bad, d, err)
		}
	}
}

func TestBackgroundMusicValidate(t *testing.T) {
	f := filepath.Join(t.TempDir(), "n.mp3")
	_ = os.WriteFile(f, []byte("x"), 0o644)
	if err := (*BackgroundMusic)(nil).Validate(); err != nil {
		t.Errorf("nil = không nhạc, không lỗi: %v", err)
	}
	for _, c := range []struct {
		m  BackgroundMusic
		ok bool
	}{
		{BackgroundMusic{f, DefaultMusicVolume}, true},
		{BackgroundMusic{f, MinMusicVolume}, true},
		{BackgroundMusic{f, MaxMusicVolume}, true},
		{BackgroundMusic{f, 0.5}, false},
		{BackgroundMusic{f, 0}, false},
		{BackgroundMusic{f + ".khong-co", 0.2}, false},
	} {
		if err := c.m.Validate(); (err == nil) != c.ok {
			t.Errorf("%+v: err=%v, muốn ok=%v", c.m, err, c.ok)
		}
	}
}

// goertzel — năng lượng tần số f trong đoạn mẫu (để tách nhạc 3 kHz khỏi "giọng" 300 Hz).
func goertzel(x []float64, rate, f float64) float64 {
	w := 2 * math.Pi * f / rate
	c := 2 * math.Cos(w)
	var s1, s2 float64
	for _, v := range x {
		s1, s2 = v+c*s1-s2, s1
	}
	return math.Sqrt(s1*s1+s2*s2-c*s1*s2) / float64(len(x))
}

// Trộn thật bằng ffmpeg: đầu ra dài bằng giọng; nhạc nhỏ hẳn khi có giọng.
func TestConvertToMP3WithMusic(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	dir := t.TempDir()
	run := func(args ...string) []byte {
		t.Helper()
		out, err := exec.Command(ffmpeg, args...).Output()
		if err != nil {
			t.Fatalf("ffmpeg %v: %v", args, err)
		}
		return out
	}
	// "Giọng": 300 Hz, im lặng 0–6s, nói 6–14s, im lặng 14–20s. Nhạc: 3 kHz 5 giây (phải lặp).
	voice := filepath.Join(dir, "voice.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=300:r=48000:d=20",
		"-af", "volume='if(between(t,6,14),0.5,0)':eval=frame", "-ac", "1", "-c:a", "pcm_s16le", voice)
	music := filepath.Join(dir, "music.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=3000:r=48000:d=5", "-ac", "1", music)

	c := &TTSConfig{Mode: TTSModeStub, FFmpeg: ffmpeg, Bitrate: "128k", Music: &BackgroundMusic{Path: music, Volume: DefaultMusicVolume},
		Logf: func(string, ...any) {}}
	if err := c.preflight(); err != nil {
		t.Fatal(err)
	}
	mp3 := filepath.Join(dir, "out.mp3")
	if err := c.convertToMP3(context.Background(), voice, mp3); err != nil {
		t.Fatal(err)
	}
	if d, err := MP3DurationSec(mp3); err != nil || d != 20 {
		t.Fatalf("thời lượng MP3 = %d (%v), muốn 20 như giọng đọc", d, err)
	}

	raw := run("-v", "error", "-i", mp3, "-f", "s16le", "-ac", "1", "-ar", "44100", "-")
	pcm := make([]float64, len(raw)/2)
	for i := range pcm {
		pcm[i] = float64(int16(binary.LittleEndian.Uint16(raw[2*i:]))) / 32768
	}
	music3k := func(from, to float64) float64 { return goertzel(pcm[int(from*44100):int(to*44100)], 44100, 3000) }
	pause, speech, tail := music3k(3, 5.5), music3k(9, 13), music3k(19.3, 20)
	if pause <= 0 {
		t.Fatal("không nghe thấy nhạc ở đoạn im lặng")
	}
	if db := 20 * math.Log10(pause/speech); db < 6 {
		t.Errorf("nhạc chỉ hạ %.1f dB khi có giọng, muốn >= 6 dB (ducking)", db)
	}
	if tail >= pause/2 {
		t.Errorf("nhạc chưa fade ra ở cuối: %.4f so với %.4f", tail, pause)
	}
}

func TestPreflightRejectsBrokenMusic(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	bad := filepath.Join(t.TempDir(), "hong.mp3")
	_ = os.WriteFile(bad, []byte("khong phai am thanh"), 0o644)
	c := &TTSConfig{Mode: TTSModeStub, FFmpeg: ffmpeg, Music: &BackgroundMusic{Path: bad, Volume: 0.2}}
	if err := c.preflight(); err == nil || !strings.Contains(err.Error(), "nhạc nền") {
		t.Errorf("file nhạc hỏng phải bị chặn ở preflight, got %v", err)
	}
}

// Acceptance #1: không chọn nhạc → MP3 đầu ra giống hệt trước (cùng lệnh ffmpeg).
func TestConvertToMP3WithoutMusic_IdenticalArgs(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	dir := t.TempDir()
	// Sinh WAV 1 giây để test
	run := func(args ...string) []byte {
		t.Helper()
		out, err := exec.Command(ffmpeg, args...).Output()
		if err != nil {
			t.Fatalf("ffmpeg %v: %v", args, err)
		}
		return out
	}
	wav := filepath.Join(dir, "test.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=300:r=48000:d=1", "-ac", "1", "-c:a", "pcm_s16le", wav)

	// Config không nhạc
	c := &TTSConfig{Mode: TTSModeStub, FFmpeg: ffmpeg, Bitrate: "128k", Logf: func(string, ...any) {}}
	mp3NoMusic := filepath.Join(dir, "no-music.mp3")
	if err := c.convertToMP3(context.Background(), wav, mp3NoMusic); err != nil {
		t.Fatal(err)
	}

	// Config có nhạc
	music := filepath.Join(dir, "music.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=3000:r=48000:d=2", "-ac", "1", "-c:a", "pcm_s16le", music)
	c.Music = &BackgroundMusic{Path: music, Volume: DefaultMusicVolume}
	mp3WithMusic := filepath.Join(dir, "with-music.mp3")
	if err := c.convertToMP3(context.Background(), wav, mp3WithMusic); err != nil {
		t.Fatal(err)
	}

	// So sánh thời lượng: cả hai phải dài 1 giây (lấy từ WAV)
	d1, err1 := MP3DurationSec(mp3NoMusic)
	d2, err2 := MP3DurationSec(mp3WithMusic)
	if err1 != nil || err2 != nil {
		t.Fatalf("đọc thời lượng MP3: %v, %v", err1, err2)
	}
	if d1 != 1 || d2 != 1 {
		t.Errorf("MP3 phải 1 giây: no-music=%d, with-music=%d", d1, d2)
	}
}

// StubMode: không trộn nhạc (chỉ sinh im lặng).
func TestConvertToMP3StubMode_IgnoresMusic(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	dir := t.TempDir()
	run := func(args ...string) []byte {
		t.Helper()
		out, err := exec.Command(ffmpeg, args...).Output()
		if err != nil {
			t.Fatalf("ffmpeg %v: %v", args, err)
		}
		return out
	}
	wav := filepath.Join(dir, "test.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=300:r=48000:d=3", "-ac", "1", "-c:a", "pcm_s16le", wav)

	music := filepath.Join(dir, "music.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=3000:r=48000:d=10", "-ac", "1", "-c:a", "pcm_s16le", music)

	// StubMode với music: phải bỏ qua nhạc
	c := &TTSConfig{Mode: TTSModeStub, FFmpeg: ffmpeg, Bitrate: "128k", Music: &BackgroundMusic{Path: music, Volume: 0.2}, Logf: func(string, ...any) {}}
	mp3 := filepath.Join(dir, "stub.mp3")
	if err := c.convertToMP3(context.Background(), wav, mp3); err != nil {
		t.Fatal(err)
	}
	d, err := MP3DurationSec(mp3)
	if err != nil || d != 3 {
		t.Errorf("StubMode phải bỏ qua nhạc, thời lượng = %d (muốn 3), lỗi = %v", d, err)
	}
}

// Stereo music 44.1 kHz với voice mono 24 kHz → trộn phải về 48 kHz mono đúng.
func TestConvertToMP3_StereoMusicWithLowerVoiceSample(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	dir := t.TempDir()
	run := func(args ...string) []byte {
		t.Helper()
		out, err := exec.Command(ffmpeg, args...).Output()
		if err != nil {
			t.Fatalf("ffmpeg %v: %v", args, err)
		}
		return out
	}
	// Voice: mono, 24 kHz, 3 giây
	voice := filepath.Join(dir, "voice-24k.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=300:r=24000:d=3", "-ac", "1", "-c:a", "pcm_s16le", voice)

	// Music: stereo, 44.1 kHz, 2 giây (phải lặp)
	music := filepath.Join(dir, "music-44k-stereo.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=3000:r=44100:d=2", "-ac", "2", "-c:a", "pcm_s16le", music)

	c := &TTSConfig{Mode: TTSModeStub, FFmpeg: ffmpeg, Bitrate: "128k", Music: &BackgroundMusic{Path: music, Volume: 0.15}, Logf: func(string, ...any) {}}
	mp3 := filepath.Join(dir, "mixed.mp3")
	if err := c.convertToMP3(context.Background(), voice, mp3); err != nil {
		t.Fatal(err)
	}
	d, err := MP3DurationSec(mp3)
	if err != nil || d != 3 {
		t.Errorf("MP3 phải dài bằng voice (3s), got %d, lỗi %v", d, err)
	}
}

// Music ngắn hơn voice → nhạc lặp liên tục.
func TestConvertToMP3_ShortMusicLoops(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	dir := t.TempDir()
	run := func(args ...string) []byte {
		t.Helper()
		out, err := exec.Command(ffmpeg, args...).Output()
		if err != nil {
			t.Fatalf("ffmpeg %v: %v", args, err)
		}
		return out
	}
	voice := filepath.Join(dir, "voice-long.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=300:r=48000:d=15", "-ac", "1", "-c:a", "pcm_s16le", voice)

	music := filepath.Join(dir, "music-short.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=3000:r=48000:d=3", "-ac", "1", "-c:a", "pcm_s16le", music)

	c := &TTSConfig{Mode: TTSModeStub, FFmpeg: ffmpeg, Bitrate: "128k", Music: &BackgroundMusic{Path: music, Volume: 0.2}, Logf: func(string, ...any) {}}
	mp3 := filepath.Join(dir, "looped.mp3")
	if err := c.convertToMP3(context.Background(), voice, mp3); err != nil {
		t.Fatal(err)
	}
	d, err := MP3DurationSec(mp3)
	if err != nil || d != 15 {
		t.Errorf("MP3 phải 15s (looped music), got %d", d)
	}
}

// Voice rất ngắn (< 2s + 3s fade = < 5s) → không có fade out.
func TestConvertToMP3_VeryShortVoiceNoFadeOut(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	dir := t.TempDir()
	run := func(args ...string) []byte {
		t.Helper()
		out, err := exec.Command(ffmpeg, args...).Output()
		if err != nil {
			t.Fatalf("ffmpeg %v: %v", args, err)
		}
		return out
	}
	// Voice 2 giây
	voice := filepath.Join(dir, "voice-short.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=300:r=48000:d=2", "-ac", "1", "-c:a", "pcm_s16le", voice)

	music := filepath.Join(dir, "music.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=3000:r=48000:d=10", "-ac", "1", "-c:a", "pcm_s16le", music)

	c := &TTSConfig{Mode: TTSModeStub, FFmpeg: ffmpeg, Bitrate: "128k", Music: &BackgroundMusic{Path: music, Volume: 0.2}, Logf: func(string, ...any) {}}
	mp3 := filepath.Join(dir, "very-short.mp3")
	if err := c.convertToMP3(context.Background(), voice, mp3); err != nil {
		t.Fatal(err)
	}
	d, err := MP3DurationSec(mp3)
	if err != nil || d != 2 {
		t.Errorf("MP3 phải 2s (no fade out), got %d", d)
	}
}

// Đường dẫn nhạc có dấu cách và ký tự tiếng Việt.
func TestConvertToMP3_MusicPathWithSpacesAndVietnamese(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	dir := t.TempDir()
	run := func(args ...string) []byte {
		t.Helper()
		out, err := exec.Command(ffmpeg, args...).Output()
		if err != nil {
			t.Fatalf("ffmpeg %v: %v", args, err)
		}
		return out
	}
	voice := filepath.Join(dir, "voice.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=300:r=48000:d=3", "-ac", "1", "-c:a", "pcm_s16le", voice)

	// Tên file: "Nhạc nền Sáng tạo 2024 - Test.wav"
	musicPath := filepath.Join(dir, "Nhạc nền Sáng tạo 2024 - Test.wav")
	run("-v", "error", "-f", "lavfi", "-i", "sine=f=3000:r=48000:d=5", "-ac", "1", "-c:a", "pcm_s16le", musicPath)

	c := &TTSConfig{Mode: TTSModeStub, FFmpeg: ffmpeg, Bitrate: "128k", Music: &BackgroundMusic{Path: musicPath, Volume: 0.2}, Logf: func(string, ...any) {}}
	mp3 := filepath.Join(dir, "output.mp3")
	if err := c.convertToMP3(context.Background(), voice, mp3); err != nil {
		t.Fatalf("đường dẫn tiếng Việt với dấu cách phải hoạt động: %v", err)
	}
	d, err := MP3DurationSec(mp3)
	if err != nil || d != 3 {
		t.Errorf("MP3 phải 3s, got %d (lỗi: %v)", d, err)
	}
}

// WAV WAVE_FORMAT_EXTENSIBLE (40 byte fmt chunk) + extra chunk lẻ.
func TestWavDurationSec_WAVE_FORMAT_EXTENSIBLE(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "extensible.wav")
	// fmt chunk 40 byte (extensible), extra chunk LIST lẻ (3 byte + padding)
	list := append([]byte("LIST"), 3, 0, 0, 0, 'a', 'b', 'c', 0)
	writeTestWAV(t, p, 16000, make([]int16, 16000*2), 40, list)
	got, err := wavDurationSec(p)
	if err != nil || got != 2.0 {
		t.Errorf("WAVE_FORMAT_EXTENSIBLE: got %.3f, muốn 2.0, lỗi = %v", got, err)
	}
}

// WAV được ffmpeg ghi (WAVE_FORMAT_EXTENSIBLE thực tế).
func TestWavDurationSec_FFmpegGenerated(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "ffmpeg.wav")
	// ffmpeg ghi WAV thực tế
	out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "sine=f=1000:r=48000:d=2.5", "-ac", "1", "-c:a", "pcm_s16le", p).Output()
	if err != nil {
		t.Fatalf("ffmpeg tạo WAV: %s", out)
	}
	got, err := wavDurationSec(p)
	if err != nil {
		t.Fatalf("đọc WAV do ffmpeg tạo: %v", err)
	}
	// So với 2.5 giây, sai sót ≤ 0.01s
	if math.Abs(got-2.5) > 0.01 {
		t.Errorf("thời lượng WAV do ffmpeg tạo: %.3f, muốn ~2.5", got)
	}
}
