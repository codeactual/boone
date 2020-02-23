# Change Log

## v0.1.3

> This release adds missing panic-handling and updates several first/third-party dependencies.

- fix
  - Panics are allowed to bubble up without requesting tview to clean up the terminal before exit.
- feat
  - --version now prints details about the build's paths and modules.
- notable dependency changes
  - Bump github.com/gdamore/tcell to v1.3.0.
  - Bump github.com/bmatcuk/doublestar to v1.1.5.
  - Bump golang.org/x/sync to v0.0.0-20190423024810-112230192c58.
  - Bump github.com/spf13/pflag to v1.0.5.
  - Bump gopkg.in/yaml.v2 to v2.2.8.
  - Bump internal/cage/... to latest from monorepo.
- refactor
  - Move snippet-sourced third party dependencies under internal/third_party.
  - Migrate to latest cage/cli/handler API (e.g. handler.Session and handler.Input) and conventions (e.g. "func NewCommand").

## v0.1.2

- feat: init `Target.Handler.Exec.Env` config variable
- dep: update `vendor` and first-party dependencies under `internal`
- test: init `test-dep` target and use it in `.travis.yml` to install `testecho`

## v0.1.1

- test: add test fixtures missing from initial export
- test: fix inconsistent `filepath.Join` use

## v0.1.0

- feat: initial release
