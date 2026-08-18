#!/bin/bash

# Test script to verify nil-safe operator syntax is recognized

cd /home/vikasavn/go‑custom.bak

echo "=== Testing tokenization of ?. operator ==="
cat > /tmp/test_token.go << 'EOF'
package main

import (
	"fmt"
	"go/scanner"
	"go/token"
)

func main() {
	src := []byte("x?.y?.z")
	var s scanner.Scanner
	fset := token.NewFileSet()
	file := fset.AddFile("", fset.Base(), len(src))
	s.Init(file, src, nil, 0)

	for {
		pos, tok, lit := s.Scan()
		if tok == token.EOF {
			break
		}
		fmt.Printf("%s\t%s\t%q\n", fset.Position(pos), tok, lit)
	}
}
EOF

echo "Building token test..."
GOROOT=/home/vikasavn/go‑custom.bak /home/vikasavn/go‑custom.bak/bin/go run /tmp/test_token.go

echo ""
echo "=== Testing parsing of ?. operator ==="
GOROOT=/home/vikasavn/go‑custom.bak /home/vikasavn/go‑custom.bak/bin/go run test_nil_safe_parse.go
