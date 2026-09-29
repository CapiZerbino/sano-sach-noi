package bgmusic

import (
	"os"
	"path/filepath"
	"testing"
)

// Mỗi bài trong danh sách phải có file nhúng, và mỗi file nhúng phải có trong danh sách.
func TestTracksMatchEmbeddedFiles(t *testing.T) {
	entries, err := files.ReadDir("tracks")
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != len(tracks) {
		t.Fatalf("%d file nhúng nhưng %d bài trong danh sách", len(entries), len(tracks))
	}
	for _, tr := range Tracks() {
		if _, err := files.ReadFile("tracks/" + tr.ID + ".mp3"); err != nil {
			t.Errorf("%s: thiếu file nhúng: %v", tr.ID, err)
		}
		if tr.Title == "" || tr.License == "" || tr.Source == "" || tr.Seconds <= 0 {
			t.Errorf("%s: thiếu thông tin hiển thị / giấy phép: %+v", tr.ID, tr)
		}
	}
}

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	id := Tracks()[0].ID
	p, err := Extract(id, dir)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := files.ReadFile("tracks/" + id + ".mp3")
	if got, _ := os.ReadFile(p); len(got) != len(want) {
		t.Fatalf("file giải nén %d byte, muốn %d", len(got), len(want))
	}
	// File đã đúng → không ghi lại (giữ mtime).
	st1, _ := os.Stat(p)
	if _, err := Extract(id, dir); err != nil {
		t.Fatal(err)
	}
	if st2, _ := os.Stat(p); !st2.ModTime().Equal(st1.ModTime()) {
		t.Error("giải nén lại file đã đúng nội dung")
	}
	// File bị hỏng / cắt cụt → ghi lại.
	_ = os.WriteFile(p, []byte("hong"), 0o644)
	if _, err := Extract(id, dir); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(p); len(got) != len(want) {
		t.Error("không sửa lại file hỏng")
	}
	if _, err := Extract("khong-co", dir); err == nil {
		t.Error("ID lạ phải báo lỗi")
	}
	if _, err := os.Stat(filepath.Join(dir, "khong-co.mp3")); err == nil {
		t.Error("ID lạ không được tạo file")
	}
}
