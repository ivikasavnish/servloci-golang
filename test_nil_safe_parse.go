package main

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
)

func main() {
	src := `package main

type User struct {
	Name string
	Addr *Address
}

type Address struct {
	City string
}

func test() {
	var u *User
	c := u?.Addr?.City
	_ = c
}
`

	fset := token.NewFileSet()
	f, err := parser.ParseFile(fset, "test.go", src, 0)
	if err != nil {
		fmt.Printf("Parse error: %v\n", err)
		return
	}

	ast.Inspect(f, func(n ast.Node) bool {
		if sel, ok := n.(*ast.SelectorExpr); ok {
			fmt.Printf("SelectorExpr: NilSafe=%v, Sel=%v\n", sel.NilSafe, sel.Sel.Name)
		}
		return true
	})

	fmt.Println("Parse successful!")
}
