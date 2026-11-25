# Contributing to Stako

Thank you for your interest in contributing to **Stako** (`StakoCP/stako`)!

## Principles
- **License:** All contributions are licensed under the **MIT License**. Include `// SPDX-License-Identifier: MIT` at the top of new source files and sign off commits with DCO (`git commit -s`).
- **Security:** Changes touching `cmd/stako-agent`, authentication, authorization, or input validation require extra security review. Follow `docs/PRD.md` section 9 (OWASP ASVS Level 2 & CIS Ubuntu Level 1).
- **Strict CSP:** Frontend code in `web/` must never introduce inline `<script>`, inline `<style>`, or `style="..."` attributes.

## Running Checks Locally
```bash
# Backend
go vet ./...
go test ./...

# Web UI
cd web && npm install && npx astro check && npm run build
```
