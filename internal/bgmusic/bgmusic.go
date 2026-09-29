// Package bgmusic — nhạc nền kèm sẵn trong Sano để trộn vào sách nói.
//
// Chỉ nhận bản thu public domain / CC0 có nguồn kiểm chứng (cả tác phẩm lẫn bản
// ghi âm): ba bản piano của Musopen lấy từ Wikimedia Commons (xem Source của
// từng bài và desktop/licenses/THIRD-PARTY-NOTICES.md). File gốc đã chuyển sang
// MP3 mono 96 kbps 44,1 kHz cho nhẹ — nhạc chỉ làm nền, lại được chuẩn hoá độ
// lớn khi trộn (bookmaker.BackgroundMusic).
package bgmusic

import (
	"bytes"
	"embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed tracks/*.mp3
var files embed.FS

// Track — một bài nhạc nền kèm sẵn.
type Track struct {
	ID        string `json:"id"`        // khoá ổn định, lưu trong lựa chọn của người dùng
	Title     string `json:"title"`     // tên hiển thị
	Composer  string `json:"composer"`  // nhà soạn nhạc
	Performer string `json:"performer"` // người biểu diễn
	Seconds   int    `json:"seconds"`   // thời lượng (làm tròn)
	License   string `json:"license"`
	Source    string `json:"source"` // trang file trên Wikimedia Commons
}

var tracks = []Track{
	{
		ID: "satie-gymnopedie-1", Title: "Gymnopédie số 1", Composer: "Erik Satie",
		Performer: "Robin Alciatore (Musopen)", Seconds: 184, License: "Public domain",
		Source: "https://commons.wikimedia.org/wiki/File:Erik_Satie_-_gymnopedies_-_la_1_ere._lent_et_douloureux.ogg",
	},
	{
		ID: "chopin-nocturne-op9-no2", Title: "Nocturne Op. 9 số 2", Composer: "Frédéric Chopin",
		Performer: "Peter Johnston (Musopen)", Seconds: 259, License: "CC0 1.0",
		Source: "https://commons.wikimedia.org/wiki/File:Chopin_Nocturne_No._2_in_E_Flat_Major,_Op._9.ogg",
	},
	{
		ID: "chopin-nocturne-op9-no3", Title: "Nocturne Op. 9 số 3", Composer: "Frédéric Chopin",
		Performer: "Xuan He (Musopen)", Seconds: 418, License: "Public domain",
		Source: "https://commons.wikimedia.org/wiki/File:Chopin_-_Nocturne_No._3_in_B_major,_Op._9_No._3_(Xuan_He).flac",
	},
}

// Tracks trả danh sách bài kèm sẵn (bản sao, gọi bên ngoài sửa không ảnh hưởng).
func Tracks() []Track { return append([]Track(nil), tracks...) }

// Find trả bài theo ID.
func Find(id string) (Track, bool) {
	for _, t := range tracks {
		if t.ID == id {
			return t, true
		}
	}
	return Track{}, false
}

// Extract chép bài id ra dir/<id>.mp3 (ffmpeg cần đường dẫn file thật) rồi trả
// đường dẫn. Đã có đúng nội dung thì giữ nguyên, không ghi lại.
func Extract(id, dir string) (string, error) {
	if _, ok := Find(id); !ok {
		return "", fmt.Errorf("không có nhạc nền kèm sẵn %q", id)
	}
	data, err := files.ReadFile("tracks/" + id + ".mp3")
	if err != nil {
		return "", err
	}
	out := filepath.Join(dir, id+".mp3")
	if old, err := os.ReadFile(out); err == nil && bytes.Equal(old, data) {
		return out, nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// File tạm tên riêng (hai lượt giải nén cùng lúc — nghe thử và render — không
	// giẫm lên nhau), rồi đổi tên nguyên tử: không để file dở khi bị ngắt.
	f, err := os.CreateTemp(dir, id+"-*.tmp")
	if err != nil {
		return "", err
	}
	_, werr := f.Write(data)
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Rename(f.Name(), out)
	}
	if werr != nil {
		os.Remove(f.Name())
		return "", werr
	}
	return out, nil
}
