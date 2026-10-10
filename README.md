# golink

Short-link service for the Tailscale fleet. See the `golink-*` beads (`bd` CLI) for the work plan.

## Development

```sh
mise run build   # build into ./build/
mise run test    # go test ./...
mise run lint    # golangci-lint
mise run dev     # pitchfork daemon (rebuild + restart on save)
```

## Git hooks

Hooks use [hk](https://hk.jdx.dev): `go_fmt` + `golangci_lint` on pre-commit. Tests are intentionally left to CI.

```sh
mise x -- hk install   # per-repo; or run once: hk install --global --mise
```

Verify with `hk run pre-commit`; it refuses the commit when lint fails.
