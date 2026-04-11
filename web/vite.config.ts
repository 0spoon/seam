/// <reference types="vitest/config" />
import { defineConfig } from 'vite';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  define: {
    __APP_VERSION__: JSON.stringify(process.env.npm_package_version || '0.0.0'),
  },
  build: {
    chunkSizeWarningLimit: 700,
    rollupOptions: {
      output: {
        manualChunks(id) {
          if (!id.includes('node_modules')) return undefined;
          if (id.includes('/cytoscape')) return 'vendor-cytoscape';
          if (
            id.includes('/@codemirror/') ||
            id.includes('/@uiw/react-codemirror') ||
            id.includes('/@lezer/') ||
            id.includes('/codemirror/')
          ) {
            return 'vendor-codemirror';
          }
          if (id.includes('/markdown-it') || id.includes('/dompurify')) {
            return 'vendor-markdown';
          }
          if (id.includes('/motion')) return 'vendor-motion';
          if (
            id.includes('/react-dom/') ||
            id.includes('/react-router') ||
            id.includes('/scheduler/')
          ) {
            return 'vendor-react';
          }
          return undefined;
        },
      },
    },
  },
  server: {
    port: 5173,
    proxy: {
      '/api': {
        target: 'http://localhost:8088',
        changeOrigin: true,
        ws: true,
      },
    },
  },
  test: {
    globals: true,
    environment: 'jsdom',
    setupFiles: ['./src/test/setup.ts'],
    css: true,
  },
});
