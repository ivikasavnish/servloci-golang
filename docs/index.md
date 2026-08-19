---
title: servloci-golang
---

# servloci-golang

A custom fork of the Go compiler (go1.27-devel) adding language
extensions on top of upstream Go. Source: [github.com/ivikasavnish/servloci-golang](https://github.com/ivikasavnish/servloci-golang).

This site is the change book — one chapter per feature, in the order it
landed, each with its commit hash, example, implementation location, and
known limitations.

## Chapters

1. [Decorator syntax](01-decorators.html) — `@decorator`, `@decorator(args)`, stacked
2. [Nil-safe selector](02-nil-safe-selector.html) — `a?.b?.c`, Kotlin/JS-style
3. [Error-propagating call](03-try-operator.html) — `f()?`, `f()?[i]`, Rust-style
4. [Format-agnostic codec](04-codec-decorator.html) — `@codec`, generates `Encode`/`Decode`, no reflection
5. [Native gRPC services](05-rpc-decorator.html) — `@rpc`, no `.proto`/`protoc`
6. [Map dot-access sugar](06-map-dot-access.html) — `m.foo` desugars to `m["foo"]`

## Quick start

```bash
git clone https://github.com/ivikasavnish/gpp
cd gpp
./install.sh
./gpp run test_decorator_syntax.go
```

See the [gpp wrapper repo](https://github.com/ivikasavnish/gpp) for the
compiler wrapper script, install instructions, and runnable test files
for every feature below.
