# @stako/web

Web UI for the Stako hosting control panel, built with Astro, Svelte 5, and TypeScript.

## Prerequisites

- Node.js 20+ (tested on Node 24 / npm 11)

## Development

Start the Go backend (`stako-core serve --insecure-http` on `127.0.0.1:8083`), then run:

```sh
npm install
npm run dev
```

All `/api` requests are proxied to `http://127.0.0.1:8083`.

## Build & Verify

```sh
npm run build
npm run check
```

The production SPA bundle is output to `web/dist/` (embedded by `stako-core`), with strict CSP compatibility (no inline `<script>`, no inline `<style>`, no `style="..."` attributes).
