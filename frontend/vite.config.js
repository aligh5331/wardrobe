import { defineConfig } from 'vite'
import react from '@vitejs/plugin-react'
import tailwindcss from '@tailwindcss/vite'

// Dev-only proxy so the Vite dev server can reach the Go API without CORS.
// In the built/embedded app the UI and API are same-origin (ING-021), so the
// app always fetches relative `/api/...` URLs.
export default defineConfig({
  plugins: [react(), tailwindcss()],
  server: {
    proxy: {
      '/api': 'http://localhost:8080',
    },
  },
})
