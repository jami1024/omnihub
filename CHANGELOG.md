# Changelog

All notable changes to this project will be documented in this file.

The format is based on [Keep a Changelog](https://keepachangelog.com/en/1.1.0/),
and this project adheres to [Semantic Versioning](https://semver.org/spec/v2.0.0.html).

## [Unreleased]

### Added

- Initial project scaffolding (`README`, `.gitignore`, `Makefile`).
- Minimal HTTP entry point at `cmd/omnihub` with `/healthz`, `/readyz`, `/version` endpoints.
- Architecture overview document under `docs/architecture/`.
- Adopt `gin-gonic/gin` as the HTTP router and middleware stack.
- ADR 0001 records the rationale for choosing Gin over chi / Echo / Fiber.
- Add Simplified Chinese README (`README.zh-CN.md`) and cross-link with English.

[Unreleased]: https://github.com/jami1024/omnihub/commits/main
