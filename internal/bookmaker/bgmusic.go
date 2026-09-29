package bookmaker

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
)

// BackgroundMusic — nhạc nền trộn vào từng tiểu mục khi convert WAV → MP3.
//
// Mỗi tiểu mục phát lại bài nhạc từ đầu (lặp nếu giọng đọc dài hơn bài), fade
// vào 2 giây, fade ra 3 giây cuối. Nhạc được chuẩn hoá độ lớn (loudnorm) rồi
// nhân Volume, sau đó tự hạ khi có giọng đọc (sidechain ducking). Đo trên giọng
// VieNeu thật: Volume 0.05 ≈ nhạc nhỏ hơn giọng 35 dB, 0.20 ≈ 23 dB, 0.40 ≈ 17 dB
// — mức 0.20 là nền nhẹ, nghe rõ lời.
type BackgroundMusic struct {
	Path   string  // file nhạc (mp3/m4a/wav/flac/ogg… ffmpeg đọc được)
	Volume float64 // MinMusicVolume..MaxMusicVolume
}

const (
	MinMusicVolume     = 0.05
	MaxMusicVolume     = 0.40
	DefaultMusicVolume = 0.20

	musicFadeInSec  = 2.0
	musicFadeOutSec = 3.0
	// musicLoudnessLUFS — chuẩn hoá mọi bài nhạc về cùng độ lớn để một mức Volume
	// nghe như nhau với bài nào (bài gốc lệch nhau tới ~11 dB).
	musicLoudnessLUFS = -14
)

// Validate kiểm tra đường dẫn + mức âm lượng (không chạy ffmpeg).
func (m *BackgroundMusic) Validate() error {
	if m == nil {
		return nil
	}
	if !fileExistsTTS(m.Path) {
		return fmt.Errorf("không tìm thấy file nhạc nền %q", m.Path)
	}
	if m.Volume < MinMusicVolume || m.Volume > MaxMusicVolume {
		return fmt.Errorf("mức nhạc nền %.2f ngoài khoảng %.2f–%.2f", m.Volume, MinMusicVolume, MaxMusicVolume)
	}
	return nil
}

// minMusicBytes — ProbeMusic đòi ít nhất 0,1 giây âm thanh (8 kHz mono s16le):
// file có header hợp lệ nhưng không có mẫu nào vẫn "giải mã được", mà lặp
// (-stream_loop -1) một luồng rỗng thì ffmpeg treo mãi.
const minMusicBytes = 1600

// ProbeMusic giải mã thử 1 giây để báo lỗi sớm (trước khi đọc giọng cả cuốn)
// khi file nhạc hỏng, không phải âm thanh, hoặc không có âm thanh.
func ProbeMusic(ffmpeg, path string) error {
	cmd := exec.Command(ffmpeg, "-v", "error", "-t", "1", "-i", path, "-vn",
		"-ac", "1", "-ar", "8000", "-f", "s16le", "-")
	hideWindow(cmd)
	var out, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("không đọc được file nhạc nền %q: %w\n%s", path, err, truncateBytes(stderr.Bytes(), 400))
	}
	if out.Len() < minMusicBytes {
		return fmt.Errorf("file nhạc nền %q không có âm thanh", path)
	}
	return nil
}

// normalizedMusic chuẩn hoá độ lớn bài nhạc MỘT lần cho cả lượt render (loudnorm
// chậm — chạy lại ở mỗi tiểu mục thì trộn chậm ~7 lần) ra WAV 48 kHz mono tạm,
// dùng lại cho mọi tiểu mục. RenderContext xoá file khi xong (releaseMusic).
func (c *TTSConfig) normalizedMusic(ctx context.Context) (string, error) {
	if c.musicNorm != "" {
		return c.musicNorm, nil
	}
	f, err := os.CreateTemp("", "sano-nhac-nen-*.wav")
	if err != nil {
		return "", err
	}
	out := f.Name()
	f.Close()
	cmd := exec.CommandContext(ctx, c.FFmpeg, "-y", "-v", "error", "-i", c.Music.Path, "-vn",
		"-af", fmt.Sprintf("aformat=sample_rates=48000:channel_layouts=mono,loudnorm=I=%d:TP=-2:LRA=11,aresample=48000", musicLoudnessLUFS),
		"-c:a", "pcm_s16le", out)
	hideWindow(cmd)
	if b, err := cmd.CombinedOutput(); err != nil {
		os.Remove(out)
		return "", fmt.Errorf("chuẩn hoá nhạc nền %q: %w\n%s", c.Music.Path, err, truncateBytes(b, 400))
	}
	if d, err := wavDurationSec(out); err != nil || d < 0.1 {
		os.Remove(out)
		return "", fmt.Errorf("file nhạc nền %q không có âm thanh", c.Music.Path)
	}
	c.musicNorm = out
	return out, nil
}

// releaseMusic xoá file nhạc đã chuẩn hoá (nếu có).
func (c *TTSConfig) releaseMusic() {
	if c.musicNorm != "" {
		os.Remove(c.musicNorm)
		c.musicNorm = ""
	}
}

// musicMixArgs — tham số ffmpeg trộn giọng (wav) với nhạc đã chuẩn hoá
// (musicWav, xem normalizedMusic) rồi ghi MP3 như convertToMP3. durSec = thời
// lượng giọng đọc (để đặt fade ra); <= 0 thì bỏ fade ra.
func musicMixArgs(wav, mp3, musicWav string, volume, durSec float64, bitrate string) []string {
	fades := fmt.Sprintf("afade=t=in:d=%g", musicFadeInSec)
	if durSec > musicFadeInSec+musicFadeOutSec {
		fades += fmt.Sprintf(",afade=t=out:st=%.3f:d=%g", durSec-musicFadeOutSec, musicFadeOutSec)
	}
	filter := fmt.Sprintf(
		// Nhạc (đã chuẩn hoá độ lớn) → mức người dùng chọn → fade.
		"[1:a]aformat=sample_rates=48000:channel_layouts=mono,volume=%s,%s[m];"+
			// Giọng: tách 2 nhánh — một nhánh để trộn, một nhánh điều khiển ducking.
			"[0:a]aformat=sample_rates=48000:channel_layouts=mono,asplit=2[v][sc];"+
			"[m][sc]sidechaincompress=threshold=0.02:ratio=4:attack=80:release=900[d];"+
			// Dài bằng giọng đọc; limiter chặn vỡ tiếng khi đỉnh giọng + nhạc cộng lại.
			"[v][d]amix=inputs=2:duration=first:normalize=0,alimiter=limit=0.97:level=0[out]",
		strconv.FormatFloat(volume, 'f', 3, 64), fades)
	return []string{
		"-y", "-i", wav,
		"-stream_loop", "-1", "-i", musicWav,
		"-filter_complex", filter, "-map", "[out]",
		"-codec:a", "libmp3lame", "-b:a", bitrate, "-ar", "44100", "-ac", "1",
		mp3,
	}
}

// wavDurationSec đọc thời lượng file WAV PCM từ header (chunk fmt + data).
func wavDurationSec(path string) (float64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	var riff [12]byte
	if _, err := io.ReadFull(f, riff[:]); err != nil {
		return 0, err
	}
	if string(riff[0:4]) != "RIFF" || string(riff[8:12]) != "WAVE" {
		return 0, errors.New("không phải file WAV")
	}
	var byteRate uint32
	for {
		var hdr [8]byte
		if _, err := io.ReadFull(f, hdr[:]); err != nil {
			return 0, errors.New("WAV thiếu chunk data")
		}
		id, size := string(hdr[0:4]), binary.LittleEndian.Uint32(hdr[4:8])
		skip := int64(size) + int64(size&1) // chunk lẻ có 1 byte đệm
		switch id {
		case "fmt ":
			var fmtChunk [16]byte
			if size < 16 {
				return 0, errors.New("chunk fmt quá ngắn")
			}
			if _, err := io.ReadFull(f, fmtChunk[:]); err != nil {
				return 0, err
			}
			byteRate = binary.LittleEndian.Uint32(fmtChunk[8:12])
			skip -= 16
		case "data":
			if byteRate == 0 {
				return 0, errors.New("WAV thiếu chunk fmt trước data")
			}
			// Bộ ghi dạng luồng có thể để size 0 / 0xFFFFFFFF: lấy phần còn lại của file.
			if pos, err := f.Seek(0, io.SeekCurrent); err == nil {
				if st, err := f.Stat(); err == nil && (size == 0 || int64(size) > st.Size()-pos) {
					size = uint32(min(st.Size()-pos, int64(^uint32(0))))
				}
			}
			return float64(size) / float64(byteRate), nil
		}
		if _, err := f.Seek(skip, io.SeekCurrent); err != nil {
			return 0, err
		}
	}
}
