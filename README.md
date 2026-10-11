# golink

Short-link service for the Tailscale fleet. See the `golink-*` beads (`bd` CLI) for the work plan.

## Development

```sh
mise run build   # build into ./build/
mise run test    # go test ./...
mise run lint    # golangci-lint
mise run dev     # pitchfork: dev postgres + one-shot codegen + dev server
mise run dev-stop # stop the above
```

## Codegen

Generated code is committed (sqlc → `db/generated`, templ → `www/*_templ.go`).
The generate commands live in `mise.toml` as `gen:sqlc` and `gen:templ`; hk and
the pitchfork oneshot daemons both call them, so there is a single definition.

```sh
mise run gen:sqlc     # regenerate db/generated from db/queries + db/migrations
mise run gen:templ    # regenerate www/*_templ.go from www/*.templ
pitchfork start gen-sqlc  # same, as a pitchfork oneshot task
```

`mise run dev` runs the codegen oneshots before starting the server, so a
fresh checkout builds without a manual regen. pre-commit regenerates
automatically when a codegen input is staged, so stale generated code never
gets committed.

## Git hooks

Hooks use [hk](https://hk.jdx.dev): codegen + `go_fmt` + `golangci_lint` on
pre-commit. Tests are intentionally left to CI.

```sh
hk install   # per-repo; or run once: hk install --global --mise
```

Verify with `hk run pre-commit`; it refuses the commit when lint fails.
