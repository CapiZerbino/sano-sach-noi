package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"sano/internal/bgmusic"
	"sano/internal/bookmaker"
)

func TestMusicChoiceResolve(t *testing.T) {
	cache := t.TempDir()
	own := filepath.Join(t.TempDir(), "rieng.mp3")
	_ = os.WriteFile(own, []byte("x"), 0o644)
	track := bgmusic.Tracks()[0].ID

	if m, err := (MusicChoice{}).resolve(cache); err != nil || m != nil {
		t.Fatalf("không chọn gì phải là không nhạc, got %+v %v", m, err)
	}
	m, err := MusicChoice{Track: track}.resolve(cache)
	if err != nil {
		t.Fatal(err)
	}
	if m.Path != filepath.Join(cache, track+".mp3") || m.Volume != bookmaker.DefaultMusicVolume {
		t.Errorf("bài kèm sẵn: %+v", m)
	}
	if _, err := os.Stat(m.Path); err != nil {
		t.Errorf("bài kèm sẵn chưa được giải nén: %v", err)
	}
	// Track ưu tiên hơn Path.
	if m, _ := (MusicChoice{Track: track, Path: own}).resolve(cache); m == nil || m.Path == own {
		t.Errorf("Track phải thắng Path: %+v", m)
	}
	if m, err := (MusicChoice{Path: own, Volume: 0.3}).resolve(cache); err != nil || m.Path != own || m.Volume != 0.3 {
		t.Errorf("file riêng: %+v %v", m, err)
	}
	for _, bad := range []MusicChoice{
		{Track: "khong-co"},
		{Path: own + ".mat"},
		{Path: own, Volume: 0.9},
	} {
		if _, err := bad.resolve(cache); err == nil {
			t.Errorf("%+v phải báo lỗi", bad)
		}
	}
}

func TestDescribeMusic(t *testing.T) {
	dir := t.TempDir()
	if _, err := describeMusic(dir, ""); err == nil {
		t.Error("thư mục phải bị từ chối")
	}
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("máy không có ffmpeg")
	}
	bad := filepath.Join(dir, "hong.mp3")
	_ = os.WriteFile(bad, []byte("khong phai am thanh"), 0o644)
	if _, err := describeMusic(bad, ffmpeg); err == nil {
		t.Error("file hỏng phải bị từ chối")
	}
	good, err := bgmusic.Extract(bgmusic.Tracks()[0].ID, dir)
	if err != nil {
		t.Fatal(err)
	}
	if f, err := describeMusic(good, ffmpeg); err != nil || f.Name != filepath.Base(good) {
		t.Errorf("file nhạc hợp lệ: %+v %v", f, err)
	}
}
