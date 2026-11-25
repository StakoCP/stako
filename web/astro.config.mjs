import { defineConfig } from 'astro/config';
import svelte from '@astrojs/svelte';

export default defineConfig({
  integrations: [svelte()],
  output: 'static',
  build: {
    inlineStylesheets: 'never',
    assets: 'assets',
  },
  vite: {
    build: {
      assetsInlineLimit: 0,
    },
    server: {
      proxy: {
        '/api': {
          target: 'http://127.0.0.1:8083',
          changeOrigin: true,
        },
      },
    },
  },
});
