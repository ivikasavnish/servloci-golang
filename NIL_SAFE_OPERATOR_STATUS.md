# Nil-Safe Operator (`?.`) — Status

Kotlin/JS-style semantics: `a?.b?.c` evaluates to the zero value of the
final field's type if any pointer/interface in the chain is nil, otherwise
the actual value. Single return value — not Go's usual `(value, ok)` idiom.

## Status: working

```go
var user *User
city := user?.Address?.City   // "" — no panic
```

## Pipeline

1. **Lexer/parser** (`src/go/scanner`, `src/go/parser`,
   `src/cmd/compile/internal/syntax/{scanner,parser,nodes}.go`) — `?.`
   recognized as `_QuestionDot` / `QUESTION_PERIOD`, sets
   `SelectorExpr.NilSafe = true`.
2. **Unified-IR export/import** (`noder/writer.go`, `noder/reader.go`) —
   the `NilSafe` bool is encoded alongside the field selector and restored
   onto `ir.SelectorExpr.NilSafe` (field added in `ir/expr.go`).
3. **Walk-time lowering** (`walk/expr.go:walkNilSafeDot`) — for each
   `NilSafe` selector, once its type is known:
   ```go
   tmp := zero(ResultType)
   if X != nil {
       tmp = X.Sel
   }
   ```
   `X` is walked before this runs, so nested chains (`a?.b?.c`) compose
   correctly bottom-up — each link's own nil-guard already collapsed to a
   temp by the time the next link sees it.

## Constraints

- `X` must be pointer or interface typed — `?.` on a plain value type is a
  compile error (`?. requires pointer or interface, found T`), not silently
  ignored.
- Base expression evaluated exactly once even with side effects (rides the
  compiler's existing order-of-evaluation pass — verified: `getUser()?.Addr`
  calls `getUser()` once).

## Tests

- `test_nil_safety.go` — full semantics: nil chain, resolved chain, partial
  nil, mixed `?.`, all passing.
- `test_nil_safe_parse.go` — parser/AST level only (`?.` → `SelectorExpr`).
- `test_nilsafe_syntax.sh` — tokenizer + parser smoke test.
- `test_current_behavior.go` — baseline showing plain `.` still panics on
  nil (motivation for `?.`, unchanged).
