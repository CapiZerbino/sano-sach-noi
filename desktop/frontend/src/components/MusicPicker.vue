<script setup lang="ts">
// Nhạc nền ở bước Chọn giọng: không nhạc / bài kèm sẵn / file của người dùng + mức nhạc.
// Sano trộn nhạc vào từng mục khi nghe thử và khi tạo sách (tự hạ khi có giọng đọc).
import { computed, onMounted, ref } from 'vue'
import { FolderOpen, Music } from 'lucide-vue-next'
import { chooseMusic, errText, musicTracks, type MusicTrack } from '../lib/backend'
import { MAX_MUSIC_VOLUME, MIN_MUSIC_VOLUME, state } from '../lib/store'

const tracks = ref<MusicTrack[]>([])
const error = ref('')

onMounted(async () => {
  try {
    tracks.value = await musicTracks()
  } catch (e) {
    error.value = errText(e)
  }
})

const volumePct = computed({
  get: () => Math.round(state.musicVolume * 100),
  set: (v: number) => (state.musicVolume = v / 100),
})

const mmss = (s: number) => `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`

async function pickFile() {
  error.value = ''
  try {
    const f = await chooseMusic()
    if (!f) return
    state.musicPath = f.path
    state.musicName = f.name
    state.musicChoice = 'file'
  } catch (e) {
    error.value = errText(e)
  }
}
</script>

<template>
  <section class="mt-8">
    <h2 class="text-base font-semibold tracking-tight flex items-center gap-2"><Music class="w-4 h-4" /> Nhạc nền</h2>
    <p class="text-sm text-muted-foreground">
      Nhạc chạy nhẹ phía sau giọng đọc, tự nhỏ xuống khi có lời, lặp lại ở đầu mỗi mục. Nghe thử ở bước Nghe thử.
    </p>
    <div class="mt-3 grid gap-2" role="radiogroup" aria-label="Nhạc nền">
      <label class="flex items-center gap-3 rounded-lg border px-4 py-2.5 cursor-pointer"
        
        :class="state.musicChoice === '' ? 'border-primary bg-primary/5' : 'border-border hover:bg-muted/50'">
        <input v-model="state.musicChoice" type="radio" value="" class="h-4 w-4 accent-[hsl(var(--primary))]" />
        <span class="font-medium text-sm">Không nhạc nền</span>
      </label>
      <label v-for="t in tracks" :key="t.id" class="flex items-center gap-3 rounded-lg border px-4 py-2.5 cursor-pointer"
        :class="state.musicChoice === t.id ? 'border-primary bg-primary/5' : 'border-border hover:bg-muted/50'">
        <input v-model="state.musicChoice" type="radio" :value="t.id" class="h-4 w-4 accent-[hsl(var(--primary))]" />
        <span class="flex-1">
          <span class="font-medium text-sm">{{ t.title }}</span>
          <span class="ml-2 text-[11px] rounded-full bg-muted px-2 py-0.5 text-muted-foreground">Kèm sẵn</span>
          <span class="block text-xs text-muted-foreground">{{ t.composer }} · {{ t.performer }} · {{ mmss(t.seconds) }} · {{ t.license }}</span>
        </span>
      </label>
      <label class="flex items-center gap-3 rounded-lg border px-4 py-2.5 cursor-pointer"
        :class="state.musicChoice === 'file' ? 'border-primary bg-primary/5' : 'border-border hover:bg-muted/50'">
        <!-- Chưa có file: bấm chọn mục này thì mở hộp chọn file luôn -->
        <input type="radio" :checked="state.musicChoice === 'file'" class="h-4 w-4 accent-[hsl(var(--primary))]"
          @change="state.musicPath ? (state.musicChoice = 'file') : pickFile()" />
        <span class="flex-1 min-w-0">
          <span class="font-medium text-sm">File nhạc của bạn</span>
          <span class="block text-xs text-muted-foreground truncate">{{ state.musicName || 'mp3, m4a, wav, flac, ogg…' }}</span>
        </span>
        <button class="h-8 px-3 rounded-full border border-border text-xs flex items-center gap-1.5 hover:bg-muted" @click.prevent="pickFile">
          <FolderOpen class="w-3.5 h-3.5" /> {{ state.musicPath ? 'Đổi file' : 'Chọn file' }}
        </button>
      </label>
    </div>
    <p v-if="error" class="mt-2 text-sm text-destructive">{{ error }}</p>
    <div v-if="state.musicChoice" class="mt-4 text-sm">
      <label for="music-volume" class="font-medium">Mức nhạc: {{ volumePct }}%</label>
      <div class="mt-1 flex items-center gap-3">
        <span class="text-xs text-muted-foreground">Rất nhỏ</span>
        <input id="music-volume" v-model.number="volumePct" type="range" :min="MIN_MUSIC_VOLUME * 100" :max="MAX_MUSIC_VOLUME * 100" step="1"
          class="flex-1 accent-[hsl(var(--primary))]" />
        <span class="text-xs text-muted-foreground">Rõ</span>
      </div>
    </div>
  </section>
</template>
