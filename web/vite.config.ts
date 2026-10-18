import { writeFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import vue from '@vitejs/plugin-vue'
import { defineConfig } from 'vite'

// The Go binary embeds this directory.
const outDir = fileURLToPath(new URL('../internal/webui/dist', import.meta.url))

export default defineConfig({
  plugins: [
    vue(),
    {
      // emptyOutDir also removes the placeholder that keeps the directory in git.
      name: 'keep-gitkeep',
      closeBundle() {
        writeFileSync(`${outDir}/.gitkeep`, '')
      },
    },
  ],
  build: {
    outDir,
    emptyOutDir: true,
    chunkSizeWarningLimit: 1500,
  },
  server: {
    proxy: { '/api': 'http://localhost:8080' },
  },
})
