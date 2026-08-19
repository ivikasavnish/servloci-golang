// Copyright 2016 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package noder

import (
	"errors"
	"fmt"
	"internal/buildcfg"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"cmd/compile/internal/base"
	"cmd/compile/internal/ir"
	"cmd/compile/internal/syntax"
	"cmd/compile/internal/typecheck"
	"cmd/compile/internal/types"
	"cmd/internal/objabi"
)

func LoadPackage(filenames []string) {
	base.Timer.Start("fe", "parse")

	// Limit the number of simultaneously open files.
	sem := make(chan struct{}, runtime.GOMAXPROCS(0)+10)

	noders := make([]*noder, len(filenames))
	for i := range noders {
		p := noder{
			err: make(chan syntax.Error),
		}
		noders[i] = &p
	}

	// Move the entire syntax processing logic into a separate goroutine to avoid blocking on the "sem".
	go func() {
		for i, filename := range filenames {
			filename := filename
			p := noders[i]
			sem <- struct{}{}
			go func() {
				defer func() { <-sem }()
				defer close(p.err)
				fbase := syntax.NewFileBase(filename)

				f, err := os.Open(filename)
				if err != nil {
					p.error(syntax.Error{Msg: err.Error()})
					return
				}
				defer f.Close()

				p.file, _ = syntax.Parse(fbase, f, p.error, p.pragma, syntax.CheckBranches) // errors are tracked via p.error
			}()
		}
	}()

	var lines uint
	var m posMap
	for _, p := range noders {
		for e := range p.err {
			base.ErrorfAt(m.makeXPos(e.Pos), 0, "%s", e.Msg)
		}
		if p.file == nil {
			base.ErrorExit()
		}
		lines += p.file.EOF.Line()
	}
	base.Timer.AddEvent(int64(lines), "lines")

	// Rewrite decorator syntax into true function wrapping before typechecking.
	for _, p := range noders {
		rewriteDecorators(p.file)
		rewriteTryOperator(p.file)
		rewriteSerdeDecorators(p.file)
	}

	unified(m, noders)
}

// rewriteDecorators rewrites decorated function declarations into true
// Python-style wrapping. Each decorator must accept the function as its last
// argument and return a function of the same type.
//
// For example:
//
//	@timed
//	func foo() { body }
//
// is rewritten to:
//
//	func foo() { timed(func() { body })() }
//
// Multiple decorators are applied innermost-first (bottom-up), matching
// Python semantics:
//
//	@A
//	@B
//	func foo() { body }
//
// becomes:
//
//	func foo() { A(B(func() { body }))() }
//
// Decorators with arguments use currying:
//
//	@logged("name")
//	func foo() { body }
//
// becomes:
//
//	func foo() { logged("name")(func() { body })() }
func rewriteDecorators(file *syntax.File) {
	if file == nil {
		return
	}
	for _, decl := range file.DeclList {
		fd, ok := decl.(*syntax.FuncDecl)
		if !ok || len(fd.Decorators) == 0 || fd.Body == nil || fd.Recv != nil {
			continue
		}

		pos := fd.Body.Pos()

		// Capture the original body in a new BlockStmt so that when we
		// replace fd.Body.List below, the literal's body is unaffected.
		litBody := &syntax.BlockStmt{}
		litBody.SetPos(pos)
		litBody.List = fd.Body.List
		litBody.Rbrace = fd.Body.Rbrace

		// Build fresh Field nodes for the literal's param/result lists.
		// Sharing *Name nodes between FuncDecl and FuncLit would cause the
		// typechecker to overwrite Defs entries, breaking parameter lookup.
		litParams := make([]*syntax.Field, len(fd.Type.ParamList))
		for i, f := range fd.Type.ParamList {
			nf := &syntax.Field{}
			nf.SetPos(f.Pos())
			nf.Type = f.Type // type exprs are uses, not defs — safe to share
			if f.Name != nil {
				nf.Name = syntax.NewName(f.Name.Pos(), f.Name.Value)
			}
			litParams[i] = nf
		}
		litResults := make([]*syntax.Field, len(fd.Type.ResultList))
		for i, f := range fd.Type.ResultList {
			nf := &syntax.Field{}
			nf.SetPos(f.Pos())
			nf.Type = f.Type
			if f.Name != nil {
				nf.Name = syntax.NewName(f.Name.Pos(), f.Name.Value)
			}
			litResults[i] = nf
		}
		litType := &syntax.FuncType{}
		litType.SetPos(fd.Type.Pos())
		litType.ParamList = litParams
		litType.ResultList = litResults

		funcLit := &syntax.FuncLit{}
		funcLit.SetPos(pos)
		funcLit.Type = litType
		funcLit.Body = litBody

		// Apply decorators from innermost (last) to outermost (first),
		// building: outermost(...innermost(funcLit)...)
		var wrapped syntax.Expr = funcLit
		for i := len(fd.Decorators) - 1; i >= 0; i-- {
			dec := fd.Decorators[i]
			wrap := &syntax.CallExpr{}
			wrap.SetPos(dec.At)
			wrap.ArgList = []syntax.Expr{wrapped}
			if dec.Call != nil {
				// @name(args) → name(args)(wrapped)
				wrap.Fun = dec.Call
			} else {
				// @name → name(wrapped)
				wrap.Fun = dec.Name
			}
			wrapped = wrap
		}

		// Call the resulting wrapper, forwarding the outer params.
		finalCall := &syntax.CallExpr{}
		finalCall.SetPos(pos)
		finalCall.Fun = wrapped
		for _, f := range fd.Type.ParamList {
			if f.Name != nil {
				finalCall.ArgList = append(finalCall.ArgList,
					syntax.NewName(f.Name.Pos(), f.Name.Value))
			}
		}
		if n := len(fd.Type.ParamList); n > 0 {
			if _, isVariadic := fd.Type.ParamList[n-1].Type.(*syntax.DotsType); isVariadic {
				finalCall.HasDots = true
			}
		}

		// Emit return if the function has result values.
		var stmt syntax.Stmt
		if len(fd.Type.ResultList) > 0 {
			ret := &syntax.ReturnStmt{}
			ret.SetPos(pos)
			ret.Results = finalCall
			stmt = ret
		} else {
			es := &syntax.ExprStmt{}
			es.SetPos(pos)
			es.X = finalCall
			stmt = es
		}

		fd.Body.List = []syntax.Stmt{stmt}
	}
}

// rewriteTryOperator desugars the bare error-propagating call operator
// (Fun(...)? and Fun(...)?[i]) into explicit temp-assignment plus
// early-return statements, before typechecking:
//
//	x := f()?
//
// becomes
//
//	_gppTry1, _gppTry2 := f()
//	if _gppTry2 != nil {
//		return <zero of enclosing func's other results>..., _gppTry2
//	}
//	x := _gppTry1
//
// and f()?[i] selects the i'th non-error result the same way.
//
// v1 scope: Fun must be a plain identifier naming a package-level function
// declared in the same file, so its result signature (arity, and whether
// the last result is exactly `error`) is known syntactically without
// running the typechecker. This means the operator does not chain through
// method calls (foo()?.Bar()? -- Bar's signature isn't known until after
// typecheck): any Try/TrySelect flag this pass can't resolve is simply
// left in the tree, where it falls through to types2 as an ordinary
// multi-value call in a single-value context -- a normal Go compile error,
// never a miscompile.
func rewriteTryOperator(file *syntax.File) {
	if file == nil {
		return
	}

	sigs := map[string][]syntax.Expr{}
	for _, decl := range file.DeclList {
		fd, ok := decl.(*syntax.FuncDecl)
		if !ok || fd.Recv != nil || fd.Name == nil {
			continue
		}
		sigs[fd.Name.Value] = resultTypes(fd.Type.ResultList)
	}

	for _, decl := range file.DeclList {
		fd, ok := decl.(*syntax.FuncDecl)
		if !ok || fd.Body == nil {
			continue
		}
		fd.Body.List = rewriteTryBlock(fd.Body.List, resultTypes(fd.Type.ResultList), sigs)
	}
}

func resultTypes(fields []*syntax.Field) []syntax.Expr {
	out := make([]syntax.Expr, len(fields))
	for i, f := range fields {
		out[i] = f.Type
	}
	return out
}

// rewriteTryBlock rewrites every statement in list, recursing into nested
// control-flow bodies (which share the same enclosing function, and so the
// same results), and returns the (possibly longer) replacement list.
func rewriteTryBlock(list []syntax.Stmt, results []syntax.Expr, sigs map[string][]syntax.Expr) []syntax.Stmt {
	if list == nil {
		return nil
	}
	out := make([]syntax.Stmt, 0, len(list))
	for _, stmt := range list {
		out = append(out, expandTryStmt(stmt, results, sigs)...)
	}
	return out
}

// expandTryStmt recurses into stmt's nested blocks in place, then checks
// whether stmt itself directly carries a top-level Try/TrySelect call
// (single '?' per statement -- v1 does not hoist nested occurrences deeper
// inside an expression tree). It returns the statement(s) that should
// replace stmt in its containing list.
func expandTryStmt(stmt syntax.Stmt, results []syntax.Expr, sigs map[string][]syntax.Expr) []syntax.Stmt {
	switch s := stmt.(type) {
	case *syntax.BlockStmt:
		s.List = rewriteTryBlock(s.List, results, sigs)

	case *syntax.IfStmt:
		s.Then.List = rewriteTryBlock(s.Then.List, results, sigs)
		if s.Else != nil {
			s.Else = expandTryStmt(s.Else, results, sigs)[0]
		}

	case *syntax.ForStmt:
		if s.Body != nil {
			s.Body.List = rewriteTryBlock(s.Body.List, results, sigs)
		}

	case *syntax.SwitchStmt:
		for _, cc := range s.Body {
			cc.Body = rewriteTryBlock(cc.Body, results, sigs)
		}

	case *syntax.SelectStmt:
		for _, cc := range s.Body {
			cc.Body = rewriteTryBlock(cc.Body, results, sigs)
		}

	case *syntax.ExprStmt:
		if pre, val := hoistTry(s.X, results, sigs); pre != nil {
			s.X = val
			return append(pre, s)
		}

	case *syntax.AssignStmt:
		if s.Rhs != nil {
			if pre, val := hoistTry(s.Rhs, results, sigs); pre != nil {
				s.Rhs = val
				return append(pre, s)
			}
		}

	case *syntax.ReturnStmt:
		if s.Results != nil {
			if pre, val := hoistTry(s.Results, results, sigs); pre != nil {
				s.Results = val
				return append(pre, s)
			}
		}

	case *syntax.DeclStmt:
		for _, d := range s.DeclList {
			vd, ok := d.(*syntax.VarDecl)
			if !ok || vd.Values == nil {
				continue
			}
			if pre, val := hoistTry(vd.Values, results, sigs); pre != nil {
				vd.Values = val
				return append(pre, s)
			}
		}
	}
	return []syntax.Stmt{stmt}
}

// hoistTry recognizes e as a bare Try call or TrySelect index expression at
// its outermost level and, if resolvable (see rewriteTryOperator's v1
// scope note), returns the statements that must run before e's containing
// statement plus the expression (a temp reference) to use in e's place.
// Returns (nil, e) when e isn't a resolvable Try expression.
func hoistTry(e syntax.Expr, results []syntax.Expr, sigs map[string][]syntax.Expr) ([]syntax.Stmt, syntax.Expr) {
	switch x := e.(type) {
	case *syntax.CallExpr:
		if x.Try {
			return lowerTryCall(x, results, sigs, -1)
		}
	case *syntax.IndexExpr:
		if x.TrySelect {
			if call, ok := x.X.(*syntax.CallExpr); ok {
				if idx := tryIndexValue(x.Index); idx >= 0 {
					return lowerTryCall(call, results, sigs, idx)
				}
			}
		}
	}
	return nil, e
}

func tryIndexValue(e syntax.Expr) int {
	lit, ok := e.(*syntax.BasicLit)
	if !ok || lit.Kind != syntax.IntLit {
		return -1
	}
	n, err := strconv.Atoi(lit.Value)
	if err != nil || n < 0 {
		return -1
	}
	return n
}

var tryTempSeq int

func newTryTemp(pos syntax.Pos) *syntax.Name {
	tryTempSeq++
	return syntax.NewName(pos, fmt.Sprintf("_gppTry%d", tryTempSeq))
}

// lowerTryCall builds the temp-assignment + error-check statements for a
// Try-marked call, and returns them alongside the temp expression that
// should replace the call (the sole non-error result for a bare '?', or
// the selIndex'th non-error result for '?[selIndex]').
func lowerTryCall(call *syntax.CallExpr, enclosingResults []syntax.Expr, sigs map[string][]syntax.Expr, selIndex int) ([]syntax.Stmt, syntax.Expr) {
	name, ok := call.Fun.(*syntax.Name)
	if !ok {
		return nil, call
	}
	callResults, ok := sigs[name.Value]
	if !ok {
		return nil, call
	}
	n := len(callResults)
	if n < 2 || !isErrorType(callResults[n-1]) {
		return nil, call
	}
	if len(enclosingResults) == 0 || !isErrorType(enclosingResults[len(enclosingResults)-1]) {
		return nil, call
	}
	if selIndex < 0 {
		if n != 2 {
			return nil, call // ambiguous with >1 non-error result: caller must use ?[i]
		}
		selIndex = 0
	} else if selIndex > n-2 {
		return nil, call // out of range, or tried to select the error slot itself
	}

	pos := call.Pos()

	temps := make([]*syntax.Name, n)
	lhs := make([]syntax.Expr, n)
	for i := range temps {
		// Only the selected result and the error need a real name; every
		// other result is discarded via the blank identifier so it doesn't
		// trip "declared and not used".
		if i == selIndex || i == n-1 {
			temps[i] = newTryTemp(pos)
		} else {
			temps[i] = syntax.NewName(pos, "_")
		}
		lhs[i] = temps[i]
	}
	assign := &syntax.AssignStmt{Op: syntax.Def, Lhs: tryTuple(pos, lhs), Rhs: call}
	assign.SetPos(pos)

	cond := &syntax.Operation{Op: syntax.Neq, X: temps[n-1], Y: syntax.NewName(pos, "nil")}
	cond.SetPos(pos)

	retResults := make([]syntax.Expr, len(enclosingResults))
	for i, rt := range enclosingResults[:len(enclosingResults)-1] {
		retResults[i] = tryZero(pos, rt)
	}
	retResults[len(enclosingResults)-1] = temps[n-1]

	ret := &syntax.ReturnStmt{Results: tryTuple(pos, retResults)}
	ret.SetPos(pos)

	then := &syntax.BlockStmt{List: []syntax.Stmt{ret}}
	then.SetPos(pos)
	ifStmt := &syntax.IfStmt{Cond: cond, Then: then}
	ifStmt.SetPos(pos)

	return []syntax.Stmt{assign, ifStmt}, temps[selIndex]
}

func isErrorType(t syntax.Expr) bool {
	name, ok := t.(*syntax.Name)
	return ok && name.Value == "error"
}

func tryTuple(pos syntax.Pos, list []syntax.Expr) syntax.Expr {
	if len(list) == 1 {
		return list[0]
	}
	le := &syntax.ListExpr{ElemList: list}
	le.SetPos(pos)
	return le
}

// tryZero builds *new(typ), a universal zero-value expression that works
// for any type without needing type information (which isn't available
// yet -- this pass runs before typecheck).
func tryZero(pos syntax.Pos, typ syntax.Expr) syntax.Expr {
	newCall := &syntax.CallExpr{Fun: syntax.NewName(pos, "new"), ArgList: []syntax.Expr{typ}}
	newCall.SetPos(pos)
	deref := &syntax.Operation{Op: syntax.Mul, X: newCall}
	deref.SetPos(pos)
	return deref
}

// rewriteSerdeDecorators finds struct type declarations tagged @serde and
// generates format-agnostic SerdeEncode/SerdeDecode methods for them,
// spliced into the file's declaration list before typechecking.
//
// The generated pair walks the struct's fields against the Encoder/Decoder
// interfaces (expected to already be declared in the same package -- same
// convention as @timed/@logged expecting their wrapper function in scope):
// one method pair works with any backend (JSON, a length-prefixed binary
// wire format, etc.) that implements those interfaces, so adding a new
// format later costs zero new codegen.
//
// Implementation note: rather than hand-building syntax.* AST nodes for
// every statement shape (as rewriteTryOperator does), the generated method
// bodies are assembled as Go source text and reparsed with syntax.Parse
// into a small synthetic file, whose FuncDecls are then spliced into the
// real file.DeclList. Both run at the same pre-typecheck stage, so this is
// just a more convenient way to build the same kind of AST -- far less
// error-prone than constructing dozens of statement/expression node types
// by hand for every field-type shape (string/int/slice/map/pointer/nested
// struct). A field type this pass can't handle (chan, func, interface{},
// non-string map keys) is a real compile error at the field's position,
// not a silently-dropped field.
//
// v1 scope: struct types only (no generics), no embedded/anonymous
// fields, map keys must be string. A field naming another type (e.g.
// `Address Addr`) is assumed to be another @serde struct; if it isn't,
// the generated `(v.Address).SerdeEncode(e)` call simply fails to compile
// with an ordinary "undefined method" error -- same safe-fail philosophy
// as rewriteTryOperator's unresolvable ? usages.
func rewriteSerdeDecorators(file *syntax.File) {
	if file == nil {
		return
	}
	var pm posMap
	for _, decl := range file.DeclList {
		td, ok := decl.(*syntax.TypeDecl)
		if !ok || !hasSerdeDecorator(td.Decorators) {
			continue
		}
		if len(td.TParamList) > 0 {
			base.ErrorfAt(pm.pos(td), 0, "@serde does not support generic types")
			continue
		}
		st, ok := td.Type.(*syntax.StructType)
		if !ok {
			base.ErrorfAt(pm.pos(td), 0, "@serde can only be applied to struct types")
			continue
		}

		src, ok := genSerdeSource(&pm, td.Name.Value, st)
		if !ok {
			continue // errors already reported at the offending fields
		}

		synthBase := syntax.NewFileBase(fmt.Sprintf("<serde:%s>", td.Name.Value))
		synth, err := syntax.Parse(synthBase, strings.NewReader(src), func(e error) {
			base.Fatalf("gpp: internal error: generated @serde code for %s failed to parse: %v\n---\n%s", td.Name.Value, e, src)
		}, nil, 0)
		if err != nil || synth == nil {
			base.Fatalf("gpp: internal error: generated @serde code for %s failed to parse: %v\n---\n%s", td.Name.Value, err, src)
		}
		file.DeclList = append(file.DeclList, synth.DeclList...)
	}
}

func hasSerdeDecorator(decorators []*syntax.Decorator) bool {
	for _, d := range decorators {
		if d.Name != nil && d.Name.Value == "serde" {
			return true
		}
	}
	return false
}

// serde field-type classification, decided purely from the syntax tree
// (no typecheck available yet).
type serdeKind int

const (
	serdeUnsupported serdeKind = iota
	serdeString
	serdeInt
	serdeFloat
	serdeBool
	serdeStruct // assumed another @serde-tagged struct, named by a plain identifier
	serdePointer
	serdeSlice
	serdeArray
	serdeMap
)

func classifySerdeType(t syntax.Expr) (kind serdeKind, elem syntax.Expr, mapKey syntax.Expr) {
	switch x := t.(type) {
	case *syntax.Name:
		switch x.Value {
		case "string":
			return serdeString, nil, nil
		case "int", "int8", "int16", "int32", "int64",
			"uint", "uint8", "uint16", "uint32", "uint64", "uintptr", "byte", "rune":
			return serdeInt, nil, nil
		case "float32", "float64":
			return serdeFloat, nil, nil
		case "bool":
			return serdeBool, nil, nil
		default:
			return serdeStruct, nil, nil
		}
	case *syntax.Operation:
		if x.Op == syntax.Mul && x.Y == nil {
			return serdePointer, x.X, nil
		}
	case *syntax.SliceType:
		return serdeSlice, x.Elem, nil
	case *syntax.ArrayType:
		return serdeArray, x.Elem, nil
	case *syntax.MapType:
		return serdeMap, x.Value, x.Key
	}
	return serdeUnsupported, nil, nil
}

// serdeTypeString renders a field type expression back to Go source text,
// for use in the generated code (var decls, make(), conversions). Only
// needs to handle the shapes classifySerdeType recognizes.
func serdeTypeString(t syntax.Expr) string {
	switch x := t.(type) {
	case *syntax.Name:
		return x.Value
	case *syntax.Operation:
		if x.Op == syntax.Mul && x.Y == nil {
			return "*" + serdeTypeString(x.X)
		}
	case *syntax.SliceType:
		return "[]" + serdeTypeString(x.Elem)
	case *syntax.ArrayType:
		return "[" + serdeTypeString(x.Len) + "]" + serdeTypeString(x.Elem)
	case *syntax.MapType:
		return "map[" + serdeTypeString(x.Key) + "]" + serdeTypeString(x.Value)
	case *syntax.BasicLit:
		return x.Value
	}
	return ""
}

// genSerdeSource builds the full Go source (package clause + both method
// decls) for typeName's SerdeEncode/SerdeDecode pair. ok is false if any
// field had an unsupported shape (already reported via base.ErrorfAt at
// that field's position); the caller should skip codegen for this struct
// in that case.
func genSerdeSource(pm *posMap, typeName string, st *syntax.StructType) (string, bool) {
	var enc, dec strings.Builder
	var fieldNames []string
	ctr := 0
	ok := true

	for _, f := range st.FieldList {
		if f.Name == nil || f.Name.Value == "_" {
			continue // embedded/anonymous fields unsupported in v1, silently skipped like decorator-on-method
		}
		kind, _, mapKey := classifySerdeType(f.Type)
		if kind == serdeUnsupported {
			base.ErrorfAt(pm.pos(f.Type), 0, "@serde: field %s.%s has an unsupported type for serialization", typeName, f.Name.Value)
			ok = false
			continue
		}
		if kind == serdeMap {
			if ks, isName := mapKey.(*syntax.Name); !isName || ks.Value != "string" {
				base.ErrorfAt(pm.pos(f.Type), 0, "@serde: field %s.%s: only string-keyed maps are supported", typeName, f.Name.Value)
				ok = false
				continue
			}
		}
		fieldNames = append(fieldNames, f.Name.Value)
		writeSerdeEncodeField(&enc, "v."+f.Name.Value, f.Type, &ctr)
		writeSerdeDecodeField(&dec, "v."+f.Name.Value, f.Type, &ctr)
	}
	if !ok {
		return "", false
	}

	var namesLit strings.Builder
	namesLit.WriteString("[]string{")
	for i, n := range fieldNames {
		if i > 0 {
			namesLit.WriteString(", ")
		}
		fmt.Fprintf(&namesLit, "%q", n)
	}
	namesLit.WriteString("}")

	var out strings.Builder
	fmt.Fprintf(&out, "package p\n\nfunc (v %s) SerdeEncode(e Encoder) error {\n", typeName)
	fmt.Fprintf(&out, "if err := e.EncodeStructStart(%q, %s); err != nil {\nreturn err\n}\n", typeName, namesLit.String())
	out.WriteString(enc.String())
	out.WriteString("return e.EncodeStructEnd()\n}\n\n")

	fmt.Fprintf(&out, "func (v *%s) SerdeDecode(d Decoder) error {\n", typeName)
	fmt.Fprintf(&out, "if err := d.DecodeStructStart(%q, %s); err != nil {\nreturn err\n}\n", typeName, namesLit.String())
	out.WriteString(dec.String())
	out.WriteString("return d.DecodeStructEnd()\n}\n")

	return out.String(), true
}

// writeSerdeEncodeField emits statements that encode the Go expression
// expr (of type t) via e, appending to w. Recurses for pointer/slice/
// array/map element types.
func writeSerdeEncodeField(w *strings.Builder, expr string, t syntax.Expr, ctr *int) {
	kind, elem, _ := classifySerdeType(t)
	switch kind {
	case serdeString:
		fmt.Fprintf(w, "if err := e.EncodeString(%s); err != nil {\nreturn err\n}\n", expr)
	case serdeInt:
		fmt.Fprintf(w, "if err := e.EncodeInt(int64(%s)); err != nil {\nreturn err\n}\n", expr)
	case serdeFloat:
		fmt.Fprintf(w, "if err := e.EncodeFloat(float64(%s)); err != nil {\nreturn err\n}\n", expr)
	case serdeBool:
		fmt.Fprintf(w, "if err := e.EncodeBool(%s); err != nil {\nreturn err\n}\n", expr)
	case serdeStruct:
		fmt.Fprintf(w, "if err := (%s).SerdeEncode(e); err != nil {\nreturn err\n}\n", expr)
	case serdePointer:
		fmt.Fprintf(w, "if %s == nil {\nif err := e.EncodeOptional(false); err != nil {\nreturn err\n}\n} else {\nif err := e.EncodeOptional(true); err != nil {\nreturn err\n}\n", expr)
		writeSerdeEncodeField(w, "(*"+expr+")", elem, ctr)
		w.WriteString("}\n")
	case serdeSlice, serdeArray:
		*ctr++
		ev := fmt.Sprintf("_gppElem%d", *ctr)
		fmt.Fprintf(w, "if err := e.EncodeSeqStart(len(%s)); err != nil {\nreturn err\n}\nfor _, %s := range %s {\n", expr, ev, expr)
		writeSerdeEncodeField(w, ev, elem, ctr)
		w.WriteString("}\n")
		w.WriteString("if err := e.EncodeSeqEnd(); err != nil {\nreturn err\n}\n")
	case serdeMap:
		*ctr++
		kv := fmt.Sprintf("_gppKey%d", *ctr)
		vv := fmt.Sprintf("_gppVal%d", *ctr)
		fmt.Fprintf(w, "if err := e.EncodeMapStart(len(%s)); err != nil {\nreturn err\n}\nfor %s, %s := range %s {\nif err := e.EncodeString(%s); err != nil {\nreturn err\n}\n", expr, kv, vv, expr, kv)
		writeSerdeEncodeField(w, vv, elem, ctr)
		w.WriteString("}\n")
		w.WriteString("if err := e.EncodeMapEnd(); err != nil {\nreturn err\n}\n")
	}
}

// writeSerdeDecodeField emits statements that decode a value of type t
// from d into the assignable Go expression target, appending to w.
func writeSerdeDecodeField(w *strings.Builder, target string, t syntax.Expr, ctr *int) {
	kind, elem, _ := classifySerdeType(t)
	*ctr++
	switch kind {
	case serdeString:
		tmp := fmt.Sprintf("_gppDec%d", *ctr)
		fmt.Fprintf(w, "%s, err := d.DecodeString()\nif err != nil {\nreturn err\n}\n%s = %s\n", tmp, target, tmp)
	case serdeInt:
		tmp := fmt.Sprintf("_gppDec%d", *ctr)
		fmt.Fprintf(w, "%s, err := d.DecodeInt()\nif err != nil {\nreturn err\n}\n%s = %s(%s)\n", tmp, target, serdeTypeString(t), tmp)
	case serdeFloat:
		tmp := fmt.Sprintf("_gppDec%d", *ctr)
		fmt.Fprintf(w, "%s, err := d.DecodeFloat()\nif err != nil {\nreturn err\n}\n%s = %s(%s)\n", tmp, target, serdeTypeString(t), tmp)
	case serdeBool:
		tmp := fmt.Sprintf("_gppDec%d", *ctr)
		fmt.Fprintf(w, "%s, err := d.DecodeBool()\nif err != nil {\nreturn err\n}\n%s = %s\n", tmp, target, tmp)
	case serdeStruct:
		fmt.Fprintf(w, "if err := (&%s).SerdeDecode(d); err != nil {\nreturn err\n}\n", target)
	case serdePointer:
		present := fmt.Sprintf("_gppPresent%d", *ctr)
		tmp := fmt.Sprintf("_gppPtr%d", *ctr)
		typ := serdeTypeString(elem)
		fmt.Fprintf(w, "%s, err := d.DecodeOptional()\nif err != nil {\nreturn err\n}\nif %s {\nvar %s %s\n", present, present, tmp, typ)
		writeSerdeDecodeField(w, tmp, elem, ctr)
		fmt.Fprintf(w, "%s = &%s\n} else {\n%s = nil\n}\n", target, tmp, target)
	case serdeSlice:
		n := fmt.Sprintf("_gppN%d", *ctr)
		sl := fmt.Sprintf("_gppSl%d", *ctr)
		ev := fmt.Sprintf("_gppSE%d", *ctr)
		typ := serdeTypeString(elem)
		fmt.Fprintf(w, "%s, err := d.DecodeSeqStart()\nif err != nil {\nreturn err\n}\n%s := make([]%s, %s)\nfor i := 0; i < %s; i++ {\nvar %s %s\n", n, sl, typ, n, n, ev, typ)
		writeSerdeDecodeField(w, ev, elem, ctr)
		fmt.Fprintf(w, "%s[i] = %s\n}\n", sl, ev)
		fmt.Fprintf(w, "if err := d.DecodeSeqEnd(); err != nil {\nreturn err\n}\n%s = %s\n", target, sl)
	case serdeArray:
		n := fmt.Sprintf("_gppN%d", *ctr)
		sl := fmt.Sprintf("_gppSl%d", *ctr)
		ev := fmt.Sprintf("_gppSE%d", *ctr)
		typ := serdeTypeString(elem)
		fmt.Fprintf(w, "%s, err := d.DecodeSeqStart()\nif err != nil {\nreturn err\n}\n%s := make([]%s, %s)\nfor i := 0; i < %s; i++ {\nvar %s %s\n", n, sl, typ, n, n, ev, typ)
		writeSerdeDecodeField(w, ev, elem, ctr)
		fmt.Fprintf(w, "%s[i] = %s\n}\n", sl, ev)
		fmt.Fprintf(w, "if err := d.DecodeSeqEnd(); err != nil {\nreturn err\n}\ncopy(%s[:], %s)\n", target, sl)
	case serdeMap:
		n := fmt.Sprintf("_gppN%d", *ctr)
		mp := fmt.Sprintf("_gppMp%d", *ctr)
		kv := fmt.Sprintf("_gppMk%d", *ctr)
		vv := fmt.Sprintf("_gppMv%d", *ctr)
		typ := serdeTypeString(elem)
		fmt.Fprintf(w, "%s, err := d.DecodeMapStart()\nif err != nil {\nreturn err\n}\n%s := make(map[string]%s, %s)\nfor i := 0; i < %s; i++ {\n%s, err := d.DecodeString()\nif err != nil {\nreturn err\n}\nvar %s %s\n", n, mp, typ, n, n, kv, vv, typ)
		writeSerdeDecodeField(w, vv, elem, ctr)
		fmt.Fprintf(w, "%s[%s] = %s\n}\n", mp, kv, vv)
		fmt.Fprintf(w, "if err := d.DecodeMapEnd(); err != nil {\nreturn err\n}\n%s = %s\n", target, mp)
	}
}

// trimFilename returns the "trimmed" filename of b, which is the
// absolute filename after applying -trimpath processing. This
// filename form is suitable for use in object files and export data.
//
// If b's filename has already been trimmed (i.e., because it was read
// in from an imported package's export data), then the filename is
// returned unchanged.
func trimFilename(b *syntax.PosBase) string {
	filename := b.Filename()
	if !b.Trimmed() {
		dir := ""
		if b.IsFileBase() {
			dir = base.Ctxt.Pathname
		}
		filename = objabi.AbsFile(dir, filename, base.Flag.TrimPath)
	}
	return filename
}

// noder transforms package syntax's AST into a Node tree.
type noder struct {
	file       *syntax.File
	linknames  []linkname
	pragcgobuf [][]string
	err        chan syntax.Error
}

// linkname records a //go:linkname directive.
type linkname struct {
	pos    syntax.Pos
	local  string
	remote string
}

var unOps = [...]ir.Op{
	syntax.Recv: ir.ORECV,
	syntax.Mul:  ir.ODEREF,
	syntax.And:  ir.OADDR,

	syntax.Not: ir.ONOT,
	syntax.Xor: ir.OBITNOT,
	syntax.Add: ir.OPLUS,
	syntax.Sub: ir.ONEG,
}

var binOps = [...]ir.Op{
	syntax.OrOr:   ir.OOROR,
	syntax.AndAnd: ir.OANDAND,

	syntax.Eql: ir.OEQ,
	syntax.Neq: ir.ONE,
	syntax.Lss: ir.OLT,
	syntax.Leq: ir.OLE,
	syntax.Gtr: ir.OGT,
	syntax.Geq: ir.OGE,

	syntax.Add: ir.OADD,
	syntax.Sub: ir.OSUB,
	syntax.Or:  ir.OOR,
	syntax.Xor: ir.OXOR,

	syntax.Mul:    ir.OMUL,
	syntax.Div:    ir.ODIV,
	syntax.Rem:    ir.OMOD,
	syntax.And:    ir.OAND,
	syntax.AndNot: ir.OANDNOT,
	syntax.Shl:    ir.OLSH,
	syntax.Shr:    ir.ORSH,
}

// error is called concurrently if files are parsed concurrently.
func (p *noder) error(err error) {
	p.err <- err.(syntax.Error)
}

// pragmas that are allowed in the std lib, but don't have
// a syntax.Pragma value (see lex.go) associated with them.
var allowedStdPragmas = map[string]bool{
	"go:cgo_export_static":  true,
	"go:cgo_export_dynamic": true,
	"go:cgo_import_static":  true,
	"go:cgo_import_dynamic": true,
	"go:cgo_ldflag":         true,
	"go:cgo_dynamic_linker": true,
	"go:embed":              true,
	"go:fix":                true,
	"go:generate":           true,
}

// *pragmas is the value stored in a syntax.pragmas during parsing.
type pragmas struct {
	Flag       ir.PragmaFlag // collected bits
	Pos        []pragmaPos   // position of each individual flag
	Embeds     []pragmaEmbed
	WasmImport *WasmImport
	WasmExport *WasmExport
}

// WasmImport stores metadata associated with the //go:wasmimport pragma
type WasmImport struct {
	Pos    syntax.Pos
	Module string
	Name   string
}

// WasmExport stores metadata associated with the //go:wasmexport pragma
type WasmExport struct {
	Pos  syntax.Pos
	Name string
}

type pragmaPos struct {
	Flag ir.PragmaFlag
	Pos  syntax.Pos
}

type pragmaEmbed struct {
	Pos      syntax.Pos
	Patterns []string
}

func (p *noder) checkUnusedDuringParse(pragma *pragmas) {
	for _, pos := range pragma.Pos {
		if pos.Flag&pragma.Flag != 0 {
			p.error(syntax.Error{Pos: pos.Pos, Msg: "misplaced compiler directive"})
		}
	}
	if len(pragma.Embeds) > 0 {
		for _, e := range pragma.Embeds {
			p.error(syntax.Error{Pos: e.Pos, Msg: "misplaced go:embed directive"})
		}
	}
	if pragma.WasmImport != nil {
		p.error(syntax.Error{Pos: pragma.WasmImport.Pos, Msg: "misplaced go:wasmimport directive"})
	}
	if pragma.WasmExport != nil {
		p.error(syntax.Error{Pos: pragma.WasmExport.Pos, Msg: "misplaced go:wasmexport directive"})
	}
}

// pragma is called concurrently if files are parsed concurrently.
func (p *noder) pragma(pos syntax.Pos, blankLine bool, text string, old syntax.Pragma) syntax.Pragma {
	pragma, _ := old.(*pragmas)
	if pragma == nil {
		pragma = new(pragmas)
	}

	if text == "" {
		// unused pragma; only called with old != nil.
		p.checkUnusedDuringParse(pragma)
		return nil
	}

	if strings.HasPrefix(text, "line ") {
		// line directives are handled by syntax package
		panic("unreachable")
	}

	if !blankLine {
		// directive must be on line by itself
		p.error(syntax.Error{Pos: pos, Msg: "misplaced compiler directive"})
		return pragma
	}

	switch {
	case strings.HasPrefix(text, "go:wasmimport "):
		f := strings.Fields(text)
		if len(f) != 3 {
			p.error(syntax.Error{Pos: pos, Msg: "usage: //go:wasmimport importmodule importname"})
			break
		}

		if buildcfg.GOARCH == "wasm" {
			// Only actually use them if we're compiling to WASM though.
			pragma.WasmImport = &WasmImport{
				Pos:    pos,
				Module: f[1],
				Name:   f[2],
			}
		}

	case strings.HasPrefix(text, "go:wasmexport "):
		f := strings.Fields(text)
		if len(f) != 2 {
			// TODO: maybe make the name optional? It was once mentioned on proposal 65199.
			p.error(syntax.Error{Pos: pos, Msg: "usage: //go:wasmexport exportname"})
			break
		}

		if buildcfg.GOARCH == "wasm" {
			// Only actually use them if we're compiling to WASM though.
			pragma.WasmExport = &WasmExport{
				Pos:  pos,
				Name: f[1],
			}
		}

	case strings.HasPrefix(text, "go:linkname "):
		f := strings.Fields(text)
		if !(2 <= len(f) && len(f) <= 3) {
			p.error(syntax.Error{Pos: pos, Msg: "usage: //go:linkname localname [linkname]"})
			break
		}
		// The second argument is optional. If omitted, we use
		// the default object symbol name for this and
		// linkname only serves to mark this symbol as
		// something that may be referenced via the object
		// symbol name from another package.
		var target string
		if len(f) == 3 {
			target = f[2]
		} else if base.Ctxt.Pkgpath != "" {
			// Use the default object symbol name if the
			// user didn't provide one.
			target = objabi.PathToPrefix(base.Ctxt.Pkgpath) + "." + f[1]
		} else {
			panic("missing pkgpath")
		}
		p.linknames = append(p.linknames, linkname{pos, f[1], target})

	case text == "go:embed", strings.HasPrefix(text, "go:embed "):
		args, err := parseGoEmbed(text[len("go:embed"):])
		if err != nil {
			p.error(syntax.Error{Pos: pos, Msg: err.Error()})
		}
		if len(args) == 0 {
			p.error(syntax.Error{Pos: pos, Msg: "usage: //go:embed pattern..."})
			break
		}
		pragma.Embeds = append(pragma.Embeds, pragmaEmbed{pos, args})

	case strings.HasPrefix(text, "go:cgo_import_dynamic "):
		// This is permitted for general use because Solaris
		// code relies on it in golang.org/x/sys/unix and others.
		fields := pragmaFields(text)
		if len(fields) >= 4 {
			lib := strings.Trim(fields[3], `"`)
			if lib != "" && !safeArg(lib) && !isCgoGeneratedFile(pos) {
				p.error(syntax.Error{Pos: pos, Msg: fmt.Sprintf("invalid library name %q in cgo_import_dynamic directive", lib)})
			}
			p.pragcgo(pos, text)
			pragma.Flag |= pragmaFlag("go:cgo_import_dynamic")
			break
		}
		fallthrough
	case strings.HasPrefix(text, "go:cgo_"):
		// For security, we disallow //go:cgo_* directives other
		// than cgo_import_dynamic outside cgo-generated files.
		// Exception: they are allowed in the standard library, for runtime and syscall.
		if !isCgoGeneratedFile(pos) && !base.Flag.Std {
			p.error(syntax.Error{Pos: pos, Msg: fmt.Sprintf("//%s only allowed in cgo-generated code", text)})
		}
		p.pragcgo(pos, text)
		fallthrough // because of //go:cgo_unsafe_args
	default:
		verb := text
		if i := strings.Index(text, " "); i >= 0 {
			verb = verb[:i]
		}
		flag := pragmaFlag(verb)
		const runtimePragmas = ir.Systemstack | ir.Nowritebarrier | ir.Nowritebarrierrec | ir.Yeswritebarrierrec
		if !base.Flag.CompilingRuntime && flag&runtimePragmas != 0 {
			p.error(syntax.Error{Pos: pos, Msg: fmt.Sprintf("//%s only allowed in runtime", verb)})
		}
		if flag == ir.UintptrKeepAlive && !base.Flag.Std {
			p.error(syntax.Error{Pos: pos, Msg: fmt.Sprintf("//%s is only allowed in the standard library", verb)})
		}
		if flag == 0 && !allowedStdPragmas[verb] && base.Flag.Std {
			p.error(syntax.Error{Pos: pos, Msg: fmt.Sprintf("//%s is not allowed in the standard library", verb)})
		}
		pragma.Flag |= flag
		pragma.Pos = append(pragma.Pos, pragmaPos{flag, pos})
	}

	return pragma
}

// isCgoGeneratedFile reports whether pos is in a file
// generated by cgo, which is to say a file with name
// beginning with "_cgo_". Such files are allowed to
// contain cgo directives, and for security reasons
// (primarily misuse of linker flags), other files are not.
// See golang.org/issue/23672.
// Note that cmd/go ignores files whose names start with underscore,
// so the only _cgo_ files we will see from cmd/go are generated by cgo.
// It's easy to bypass this check by calling the compiler directly;
// we only protect against uses by cmd/go.
func isCgoGeneratedFile(pos syntax.Pos) bool {
	// We need the absolute file, independent of //line directives,
	// so we call pos.Base().Pos().
	return strings.HasPrefix(filepath.Base(trimFilename(pos.Base().Pos().Base())), "_cgo_")
}

// safeArg reports whether arg is a "safe" command-line argument,
// meaning that when it appears in a command-line, it probably
// doesn't have some special meaning other than its own name.
// This is copied from SafeArg in cmd/go/internal/load/pkg.go.
func safeArg(name string) bool {
	if name == "" {
		return false
	}
	c := name[0]
	return '0' <= c && c <= '9' || 'A' <= c && c <= 'Z' || 'a' <= c && c <= 'z' || c == '.' || c == '_' || c == '/' || c >= utf8.RuneSelf
}

// parseGoEmbed parses the text following "//go:embed" to extract the glob patterns.
// It accepts unquoted space-separated patterns as well as double-quoted and back-quoted Go strings.
// go/build/read.go also processes these strings and contains similar logic.
func parseGoEmbed(args string) ([]string, error) {
	var list []string
	for args = strings.TrimSpace(args); args != ""; args = strings.TrimSpace(args) {
		var path string
	Switch:
		switch args[0] {
		default:
			i := len(args)
			for j, c := range args {
				if unicode.IsSpace(c) {
					i = j
					break
				}
			}
			path = args[:i]
			args = args[i:]

		case '`':
			i := strings.Index(args[1:], "`")
			if i < 0 {
				return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args)
			}
			path = args[1 : 1+i]
			args = args[1+i+1:]

		case '"':
			i := 1
			for ; i < len(args); i++ {
				if args[i] == '\\' {
					i++
					continue
				}
				if args[i] == '"' {
					q, err := strconv.Unquote(args[:i+1])
					if err != nil {
						return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args[:i+1])
					}
					path = q
					args = args[i+1:]
					break Switch
				}
			}
			if i >= len(args) {
				return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args)
			}
		}

		if args != "" {
			r, _ := utf8.DecodeRuneInString(args)
			if !unicode.IsSpace(r) {
				return nil, fmt.Errorf("invalid quoted string in //go:embed: %s", args)
			}
		}
		list = append(list, path)
	}
	return list, nil
}

// A function named init is a special case.
// It is called by the initialization before main is run.
// To make it unique within a package and also uncallable,
// the name, normally "pkg.init", is altered to "pkg.init.0".
var renameinitgen int

func Renameinit() *types.Sym {
	s := typecheck.LookupNum("init.", renameinitgen)
	renameinitgen++
	return s
}

func checkEmbed(decl *syntax.VarDecl, haveEmbed, withinFunc bool) error {
	switch {
	case !haveEmbed:
		return errors.New("go:embed requires import \"embed\" (or import _ \"embed\", if package is not used)")
	case len(decl.NameList) > 1:
		return errors.New("go:embed cannot apply to multiple vars")
	case decl.Values != nil:
		return errors.New("go:embed cannot apply to var with initializer")
	case decl.Type == nil:
		// Should not happen, since Values == nil now.
		return errors.New("go:embed cannot apply to var without type")
	case withinFunc:
		return errors.New("go:embed cannot apply to var inside func")
	case !types.AllowsGoVersion(1, 16):
		return fmt.Errorf("go:embed requires go1.16 or later (-lang was set to %s; check go.mod)", base.Flag.Lang)

	default:
		return nil
	}
}
