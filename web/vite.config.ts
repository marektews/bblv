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
    rolldownOptions: {
      output: {
        // The libraries change seldom, the application with every update;
        // kept apart, a page reloading after an update fetches only the
        // application again. Three.js stays in the 3D view, loaded when a
        // printer is opened.
        codeSplitting: {
          groups: [
            {
              name: 'vendor',
              test: (id: string) => /[\\/]node_modules[\\/]/.test(id) && !/[\\/]node_modules[\\/]three[\\/]/.test(id),
            },
          ],
        },
      },
    },
    // Just above the largest chunk, Element Plus in vendor, so that growth shows.
    chunkSizeWarningLimit: 1300,
  },
  server: {
    // bblv serve uses HTTPS with a self-signed certificate by default.
    proxy: { '/api': { target: 'https://localhost:8443', secure: false } },
  },
})
