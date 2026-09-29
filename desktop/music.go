package main

// Nhạc nền sách nói: chọn bài kèm sẵn hoặc file nhạc của người dùng, trộn vào
// từng tiểu mục lúc render / nghe thử (bookmaker.BackgroundMusic).

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"

	"sano/internal/bgmusic"
	"sano/internal/bookmaker"
)

// MusicChoice — lựa chọn nhạc nền ở bước Chọn giọng. Track (bài kèm sẵn) được
// ưu tiên hơn Path (file riêng); cả hai trống = không nhạc.
type MusicChoice struct {
	Track  string  `json:"track"`
	Path   string  `json:"path"`
	Volume float64 `json:"volume"` // 0 = mức mặc định
}

// MusicFile — file nhạc người dùng vừa chọn.
type MusicFile struct {
	Path string `json:"path"`
	Name string `json:"name"`
}

// musicExts — định dạng cho phép trong hộp chọn file (ffmpeg đọc được hết).
var musicExts = []string{".mp3", ".m4a", ".aac", ".wav", ".flac", ".ogg", ".opus"}

// MusicTracks — danh sách nhạc nền kèm sẵn.
func (a *App) MusicTracks() []bgmusic.Track { return bgmusic.Tracks() }

// ChooseMusic mở hộp chọn file nhạc. Huỷ → (nil, nil). Giải mã thử ngay để báo
// file hỏng trước khi người dùng đi tiếp.
func (a *App) ChooseMusic() (*MusicFile, error) {
	if a.ctx == nil {
		return nil, errors.New("ứng dụng chưa khởi động xong")
	}
	pattern := "*" + strings.Join(musicExts, ";*")
	path, err := wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:   "Chọn nhạc nền",
		Filters: []wruntime.FileFilter{{DisplayName: "File nhạc (" + strings.ReplaceAll(pattern, ";", ", ") + ")", Pattern: pattern}},
	})
	if err != nil {
		return nil, fmt.Errorf("mở hộp chọn file: %w", err)
	}
	if path == "" {
		return nil, nil
	}
	return describeMusic(path, findFFmpeg())
}

func describeMusic(path, ffmpeg string) (*MusicFile, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("đọc file nhạc: %w", err)
	}
	if info.IsDir() {
		return nil, errors.New("đây là thư mục, không phải file nhạc")
	}
	if ffmpeg != "" {
		if err := bookmaker.ProbeMusic(ffmpeg, path); err != nil {
			return nil, fmt.Errorf("không đọc được %q như một file nhạc", filepath.Base(path))
		}
	}
	return &MusicFile{Path: path, Name: filepath.Base(path)}, nil
}

// resolve đổi lựa chọn thành nhạc nền cho bộ đọc; bài kèm sẵn được giải nén vào
// cacheDir. nil = không nhạc.
func (m MusicChoice) resolve(cacheDir string) (*bookmaker.BackgroundMusic, error) {
	vol := m.Volume
	if vol == 0 {
		vol = bookmaker.DefaultMusicVolume
	}
	var path string
	switch {
	case strings.TrimSpace(m.Track) != "":
		p, err := bgmusic.Extract(strings.TrimSpace(m.Track), cacheDir)
		if err != nil {
			return nil, fmt.Errorf("nhạc nền: %w", err)
		}
		path = p
	case strings.TrimSpace(m.Path) != "":
		path = strings.TrimSpace(m.Path)
	default:
		return nil, nil
	}
	bm := &bookmaker.BackgroundMusic{Path: path, Volume: vol}
	if err := bm.Validate(); err != nil {
		return nil, err
	}
	return bm, nil
}

// bookOptions — options của BookSettings kèm nhạc nền (dùng chung cho nghe thử và render).
func (a *App) bookOptions(s BookSettings, t toolPaths, outDir string) (bookmaker.Options, error) {
	opts, err := s.options(t, outDir, a.globalDict())
	if err != nil {
		return opts, err
	}
	opts.TTS.Music, err = s.Music.resolve(filepath.Join(a.lib.Root(), ".tam", "nhac-nen"))
	return opts, err
}
