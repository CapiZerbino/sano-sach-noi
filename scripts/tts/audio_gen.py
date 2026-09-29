#!/usr/bin/env python3
"""
Đọc giọng 1 file văn bản bằng VieNeu-TTS v3 Turbo (ONNX/CPU, 48 kHz, giọng dựng sẵn).

v3 Turbo tự chia câu, tự chống "nói thêm" (babble guard) và tự chèn khoảng nghỉ
theo ranh giới: ngắt đoạn (dòng trắng / xuống dòng) 0,70s > hết câu 0,50s > ngắt
trong câu 0,30s. Vì vậy script chỉ đưa NGUYÊN văn bản vào `tts.infer()`.

Khoảng nghỉ 0,30s trong câu chỉ có khi v3 Turbo phải cắt câu (dài hơn max_chars).
Để mặc định 256 thì câu dài nhiều dấu phẩy thường nằm gọn trong một đoạn, model
tự quyết ngắt hay không, có lúc đọc một tràng. MAX_CHARS nhỏ hơn buộc cắt ở dấu
phẩy nên ngắt đều hơn, thời lượng gần như không đổi.

Model nạp theo revision ghim trong versions.env (xem models.py) và chạy offline
sau lần tải đầu.

Usage (chạy bằng python của venv VieNeu-TTS v3):
    python audio_gen.py <input.txt> [voice_name] [output.wav]

Example:
    python audio_gen.py chuong1.txt "Hải Đăng" chuong1_full.wav
"""

import re
import sys
import time
from pathlib import Path

# In tiếng Việt + emoji an toàn trên console Windows (mặc định cp1252).
for _stream in (sys.stdout, sys.stderr):
    try:
        _stream.reconfigure(encoding="utf-8", errors="replace")
    except (AttributeError, ValueError):
        pass

import custom_voices
import models

DEFAULT_VOICE = "Hải Đăng"
MAX_CHARS = 90

# Phiên âm ghi đè theo từ, RIÊNG TỪNG GIỌNG: tên giọng → {từ viết thường → phiên âm
# IPA kiểu sea_g2p}. Giọng không có trong bảng đọc như thường.
#
# Vì sao: bộ phiên âm sea_g2p của VieNeu gộp "ch" và "tr" thành cùng một âm tʃ,
# nên "chánh" (chánh niệm, chánh kiến, chánh định...) và "tránh" có phiên âm giống
# hệt nhau (tʃˈe-ɜɲ), model tự chọn cách đọc. Giọng Thiền Tâm Đức đọc thành
# "tránh" (nghe kiểm 28/09/2026), đổi cách viết cũng không sửa được; các giọng khác
# (Hải Đăng, Ngọc Huyền...) nghe vẫn đúng "chánh" nên không ghi đè. Phiên âm riêng
# dưới đây đã nghe kiểm với Thiền Tâm Đức: "chánh" rõ, "tránh" chỗ khác vẫn đúng.
# Lưu ý: Whisper nhận dạng ch/tr với giọng VieNeu không tin được, phải nghe tai.
PHONEME_OVERRIDES = {
    "Thiền Tâm Đức": {"chánh": "tʃˈeɜɲ"},
}

# Bảng ghi đè của giọng đang đọc (set_voice_overrides đặt trước mỗi lần đọc).
_active_overrides = {}

_SENTINEL_WORD = "khuỵp"
_SENTINEL_PHONEME_RE = re.compile(r"xw[ˈˌ]?i6p")


def clean_text(text):
    """Bỏ dòng tiêu đề markdown (# ...) — không đọc."""
    return re.sub(r'^#+\s.*$', '', text, flags=re.MULTILINE).strip()


def set_voice_overrides(voice):
    """Chọn bảng phiên âm ghi đè theo giọng sắp đọc (không có → không ghi đè)."""
    global _active_overrides
    _active_overrides = PHONEME_OVERRIDES.get(voice, {})


def _with_phoneme_overrides(phonemize):
    """Bọc hàm phiên âm của VieNeu: từ trong bảng ghi đè của giọng đang đọc (nguyên
    từ, không phân biệt hoa thường) được thay bằng phiên âm riêng, phần còn lại
    phiên âm như thường. Lệch số lượng (bộ phiên âm bỏ mất từ đánh dấu) thì trả
    về phiên âm gốc."""

    def patched(text):
        overrides = _active_overrides
        if not overrides:
            return phonemize(text)
        words = "|".join(re.escape(w) for w in sorted(overrides, key=len, reverse=True))
        word_re = re.compile(rf"(?<!\w)(?:{words})(?!\w)", re.IGNORECASE)
        found = []

        def mark(m):
            found.append(overrides[m.group(0).lower()])
            return _SENTINEL_WORD

        marked = word_re.sub(mark, text)
        if not found:
            return phonemize(text)
        it = iter(found)
        out, n = _SENTINEL_PHONEME_RE.subn(lambda m: next(it), phonemize(marked))
        if n != len(found):
            return phonemize(text)
        return out

    patched._sano_overrides = True
    return patched


def install_phoneme_overrides():
    """Gắn PHONEME_OVERRIDES vào đường phiên âm của VieNeu v3 Turbo (mọi lối
    infer / infer_stream đều qua hàm này). Gọi nhiều lần vô hại."""
    import vieneu.v3turbo as v3

    if not getattr(v3.phonemize_text_with_emotions, "_sano_overrides", False):
        v3.phonemize_text_with_emotions = _with_phoneme_overrides(v3.phonemize_text_with_emotions)


def load_tts():
    """Nạp VieNeu v3 Turbo với model ghim revision (không cần mạng sau lần tải đầu)
    kèm các giọng riêng đã lưu."""
    models.activate_offline()
    from vieneu import Vieneu
    tts = Vieneu(mode="v3turbo")
    install_phoneme_overrides()
    custom_voices.load_into(tts)  # giọng riêng nhân bản từ clip mẫu, gọi theo tên như giọng dựng sẵn
    return tts


def voice_names(tts):
    return list(tts._preset_voices)


def print_voices(tts):
    print("\n📋 Giọng có sẵn:")
    for label, _ in tts.list_preset_voices():
        print(f"   • {label}")


def pick_voice(tts, voice_query=None):
    """Trả TÊN giọng preset: khớp đúng tên/bí danh, rồi khớp một phần (không phân
    biệt hoa thường). Không thấy → báo lỗi kèm danh sách (không tự đổi giọng)."""
    query = (voice_query or DEFAULT_VOICE).strip()
    name = tts.resolve_voice_name(query)
    if name is None:
        q = query.casefold()
        name = next((n for n in voice_names(tts) if q in n.casefold()), None)
    if name is None:
        print_voices(tts)
        raise SystemExit(f"❌ Không có giọng '{query}'.")
    print(f"✅ Giọng: {name}")
    return name


def synth_file(tts, voice, input_path, output_path):
    """Đọc cả file → WAV 48 kHz. Trả (thời lượng audio giây, thời gian đọc giây)."""
    import soundfile as sf

    text = clean_text(Path(input_path).read_text(encoding="utf-8"))
    if not text:
        raise ValueError(f"File rỗng: {input_path}")
    set_voice_overrides(voice)
    start = time.time()
    wav = tts.infer(text=text, voice=voice, max_chars=MAX_CHARS)
    compute = time.time() - start
    if wav is None or len(wav) == 0:
        raise RuntimeError(f"VieNeu không sinh được audio cho {input_path}")
    sf.write(str(output_path), wav, tts.sample_rate)
    return len(wav) / tts.sample_rate, compute


def main():
    if len(sys.argv) < 2:
        print('Usage: python audio_gen.py <input.txt> [voice_name] [output.wav]')
        print('Example: python audio_gen.py chuong1.txt "Hải Đăng" chuong1_full.wav')
        sys.exit(1)

    input_file = Path(sys.argv[1])
    voice_query = sys.argv[2] if len(sys.argv) > 2 else DEFAULT_VOICE
    output = Path(sys.argv[3]) if len(sys.argv) > 3 else Path(input_file.stem + "_full.wav")

    print("🎤 Loading VieNeu v3 Turbo...")
    t0 = time.time()
    tts = load_tts()
    voice = pick_voice(tts, voice_query)
    print(f"   Loaded trong {time.time() - t0:.0f}s")

    duration, compute = synth_file(tts, voice, input_file, output)
    print(f"\n✨ Done! Output: {output}")
    print(f"   Audio: {duration:.1f}s — đọc mất {compute:.1f}s ({compute / duration:.2f}x realtime)")


if __name__ == "__main__":
    main()
