#!/usr/bin/env python3
"""
Giọng riêng (nhân bản từ clip mẫu) cho VieNeu-TTS v3 Turbo.

VieNeu cho nhân bản giọng từ một clip 3–8 giây (`add_voice`: khử nhiễu, cắt còn
≤ 8 giây, trích hồ sơ giọng = speaker embedding + reference codes). Module này lưu
hồ sơ đó vào file JSON của Sano để lần sau chỉ cần gọi theo tên như giọng dựng sẵn,
không phải nạp lại clip.

Vì sao không dùng `save_voices()` của VieNeu: hàm đó ghi đè file giọng nằm trong
mã VieNeu (mất khi cài lại bộ đọc) và chép luôn cả 23 giọng dựng sẵn. Ở đây chỉ
lưu giọng riêng, trong thư mục dữ liệu của Sano — gỡ / cài lại bộ đọc không mất.

Vị trí file (cùng quy tắc thư mục dữ liệu của app, xem desktop/internal/tts/layout.go):
    SANO_DATA_DIR/giong-rieng/voices_v3_turbo.json nếu đặt SANO_DATA_DIR, không thì
    macOS   ~/Library/Application Support/Sano/giong-rieng/...
    Windows %LOCALAPPDATA%\\Sano\\giong-rieng\\...
    Linux   $XDG_DATA_HOME/sano/giong-rieng/... (mặc định ~/.local/share/sano)
"""

import json
import os
import sys
from pathlib import Path

VOICES_SUBDIR = "giong-rieng"
VOICES_FILE = "voices_v3_turbo.json"


def data_dir():
    """Thư mục dữ liệu của Sano (khớp tts.DataDir phía Go)."""
    env = os.environ.get("SANO_DATA_DIR", "").strip()
    if env:
        return Path(env)
    home = Path.home()
    if sys.platform == "darwin":
        return home / "Library" / "Application Support" / "Sano"
    if sys.platform.startswith("win"):
        local = os.environ.get("LOCALAPPDATA", "").strip()
        return Path(local) / "Sano" if local else home / "AppData" / "Local" / "Sano"
    xdg = os.environ.get("XDG_DATA_HOME", "").strip()
    return Path(xdg) / "sano" if xdg and os.path.isabs(xdg) else home / ".local" / "share" / "sano"


def voices_path():
    return data_dir() / VOICES_SUBDIR / VOICES_FILE


def _read():
    path = voices_path()
    if not path.is_file():
        return {}
    try:
        return json.loads(path.read_text(encoding="utf-8")).get("presets", {})
    except (OSError, ValueError) as e:
        print(f"⚠️  Bỏ qua file giọng riêng hỏng {path}: {e}")
        return {}


def _write(presets):
    path = voices_path()
    path.parent.mkdir(parents=True, exist_ok=True)
    tmp = path.with_suffix(".tmp")
    tmp.write_text(json.dumps({"presets": presets}, ensure_ascii=False), encoding="utf-8")
    os.replace(tmp, path)  # ghi nguyên tử: không để file dở dang nếu bị ngắt giữa chừng


def load_into(tts):
    """Nạp các giọng riêng đã lưu vào model (gọi ngay sau khi nạp VieNeu).

    Ghi thẳng vào bảng giọng của VieNeu với đúng cấu trúc `add_voice` tạo ra
    (bản VieNeu ghim trong versions.env). Trùng tên giọng dựng sẵn thì giọng riêng thắng.
    """
    import numpy as np

    for name, v in _read().items():
        if v.get("speaker_emb") is None:
            continue
        tts._preset_voices[name] = {
            "description": v.get("description", ""),
            "gender": v.get("gender", ""),
            "style": tts.default_style,
            "speaker_emb": np.asarray(v["speaker_emb"], dtype=np.float32),
            "codes": None if v.get("codes") is None else np.asarray(v["codes"], dtype=np.int64),
        }


MIN_REF_SECONDS = 3.0
# Đoạn mẫu (ref codes) dài hơn 8 giây mặc định của VieNeu: đo trên giọng thật, 16
# giây giống giọng hơn một chút; 20 giây lại kém đi và đọc nhanh bất thường (vượt
# độ dài model được huấn luyện) → chặn ở 16.
MAX_REF_SECONDS = 16.0
EMB_WINDOW_SECONDS = 8.0


def _speaker_emb_average(eng, wav, sr):
    """Hồ sơ giọng (speaker embedding) trung bình trên các khúc 8 giây của cả clip —
    clip dài (vài phút) cho hồ sơ ổn định hơn một khúc ngắn."""
    import tempfile

    import numpy as np
    import soundfile as sf

    step = int(EMB_WINDOW_SECONDS * sr)
    embs = []
    with tempfile.TemporaryDirectory() as tmp:
        for i in range(0, len(wav) - int(MIN_REF_SECONDS * sr) + 1, step):
            path = Path(tmp) / f"w{i}.wav"
            sf.write(str(path), wav[i:i + step], sr)
            e, _ = eng.prepare_reference(str(path), use_ref_codes=False)  # khử nhiễu như add_voice
            embs.append(np.asarray(e, dtype=np.float32).reshape(-1))
    return np.mean(embs, axis=0), len(embs)


def add(tts, name, clip, description="Giọng riêng", ref_seconds=8.0):
    """Nhân bản giọng từ `clip` rồi lưu.

    Đoạn mẫu = `ref_seconds` giây ĐẦU của clip (3–16; nên là lời nói liền mạch, ít
    tiếng nền). Clip dài hơn thế thì hồ sơ giọng lấy trung bình trên cả clip.
    """
    import numpy as np
    import soundfile as sf

    name = name.strip()
    clip = Path(clip)
    if not name:
        raise SystemExit("❌ Cần đặt tên cho giọng riêng.")
    if not clip.is_file():
        raise SystemExit(f"❌ Không thấy clip mẫu: {clip}")
    if not MIN_REF_SECONDS <= ref_seconds <= MAX_REF_SECONDS:
        raise SystemExit(f"❌ Độ dài đoạn mẫu phải từ {MIN_REF_SECONDS:g} đến {MAX_REF_SECONDS:g} giây.")

    wav, sr = sf.read(str(clip), dtype="float32", always_2d=True)
    wav = wav.mean(axis=1)  # về mono
    seconds = len(wav) / sr
    if seconds < MIN_REF_SECONDS:
        raise SystemExit(f"❌ Clip mẫu quá ngắn ({seconds:.1f} giây), cần ít nhất {MIN_REF_SECONDS:g} giây.")

    eng = tts.engine
    emb, codes = eng.prepare_reference(str(clip), max_seconds=ref_seconds)  # khử nhiễu + cắt
    windows = 1
    if seconds > ref_seconds + EMB_WINDOW_SECONDS:
        emb, windows = _speaker_emb_average(eng, wav, sr)
    emb = np.asarray(emb, dtype=np.float32).reshape(-1)

    tts._preset_voices[name] = {  # cùng cấu trúc add_voice của VieNeu
        "description": description, "gender": "", "style": tts.default_style,
        "speaker_emb": emb, "codes": None if codes is None else np.asarray(codes, dtype=np.int64),
    }
    presets = _read()
    presets[name] = {
        "description": description,
        "speaker_emb": [round(float(x), 6) for x in emb],
        "codes": None if codes is None else np.asarray(codes, dtype=int).tolist(),
    }
    _write(presets)
    print(f"✅ Đã lưu giọng riêng '{name}' (đoạn mẫu {min(seconds, ref_seconds):.0f}s, "
          f"hồ sơ giọng từ {windows} khúc) → {voices_path()}")


def remove(name):
    presets = _read()
    if presets.pop(name.strip(), None) is None:
        raise SystemExit(f"❌ Không có giọng riêng '{name}'.")
    _write(presets)
    print(f"🗑️  Đã xoá giọng riêng '{name}'.")
