# servloci-golang change book

This directory is the change book for the `servloci-golang` fork — every
language/toolchain addition on top of upstream Go, one chapter per feature,
in the order it landed. It follows the same convention as this repo's own
`doc/next/` (see `../README.md`): numbered files, `## Title {#anchor}`
headers, meant to be read standalone or concatenated in order.

Unlike `doc/next/`, which is upstream Go's own in-progress release notes
and gets flushed at each real Go release, this directory is permanent —
it's the fork's own history, not staging for anything upstream.

Each chapter carries the commit hash it landed in, so `git show <hash>`
always gets you the exact diff behind the prose.

## Chapters

| # | Feature | Commit | Date |
|---|---|---|---|
| 1 | [Decorator syntax](01-decorators.md) (`@decorator`) | `71aa26f4bd` | 2026-08-18 |
| 2 | [Nil-safe selector](02-nil-safe-selector.md) (`?.`) | `71aa26f4bd` | 2026-08-18 |
| 3 | [Error-propagating call](03-try-operator.md) (`?`, `?[i]`) | `01b9444681` | 2026-08-19 |
| 4 | [Format-agnostic codec](04-codec-decorator.md) (`@codec`, was `@serde`) | `dd1e76a7bb` → renamed `0875646a24` | 2026-08-19 |
| 5 | [Native gRPC services](05-rpc-decorator.md) (`@rpc`) | `c9932ad0f8` | 2026-08-19 |
| 6 | [Map dot-access sugar](06-map-dot-access.md) (`m.foo`) | `c02aa91156` | 2026-08-19 |

## Adding a chapter

When a new language/toolchain feature lands:

1. Add `NN-feature-name.md` here, following the shape of the existing
   chapters — summary, example, implementation location, known
   limitations, commit hash.
2. Add a row to the table above.
3. Update the "Additions" section of `../../README.md` with the
   user-facing summary (the change book chapter can be longer/more
   technical than the README section).
