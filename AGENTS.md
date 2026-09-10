# AGENTS.md

## Repository

- This repository is a Go CLI module: `github.com/xkumiyu/catsift`.
- The CLI entrypoint is `cmd/catsift`; implementation packages are under `internal/`.
- Keep Go tests beside their packages as `*_test.go`; fuzz tests use `*_fuzz_test.go`.
- `npm/` contains the Node.js distribution wrapper and its tests.

## Development

- Use the repository's `mise` tasks for development. After changes, run `mise run check`; it checks Go formatting, lint, vet, race-enabled tests, the build, npm wrapper tests, and npm package contents. Use `mise run fmt` to apply Go formatting when needed.
- Run `mise run release-check` when changing `.goreleaser.yaml`.

## Tests and data

- Do not store or commit real external data in Git-tracked files. Tests, fixtures, examples, and generated artifacts must use synthetic or sanitized repository-local data; runtime reads of user-selected external data are allowed, but tests must not depend on a developer's private data.

## Documentation and generated files

- Keep `README.md` and `README.ja.md` content-equivalent and update them together. Their prose may be translated, but commands, options, examples, and section coverage must stay in sync.
- OpenSpec artifacts under `openspec/` follow `openspec/config.yaml`, which requires Japanese artifacts.
- `.agents/skills/` is materialized by APM from `apm.yml` and `apm.lock.yaml`; do not hand-edit deployed skill files.
- Treat `go.mod` as the Go dependency manifest and `go.sum` as generated checksums; update both with Go tooling. Treat `apm.lock.yaml` as an APM lock file and update it with APM tooling.
