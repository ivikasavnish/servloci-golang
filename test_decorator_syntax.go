package main

import (
	"fmt"
	"time"
)

// timed wraps fn to measure its execution time.
// Signature: func(func()) func() — takes the function, returns a wrapped version.
func timed(fn func()) func() {
	return func() {
		start := time.Now()
		fn()
		fmt.Printf("  [timed: %v]\n", time.Since(start))
	}
}

// logged wraps fn to log calls by name.
// Curried: logged("name") returns a func(func()) func().
func logged(name string) func(func()) func() {
	return func(fn func()) func() {
		return func() {
			fmt.Printf("  [logged: → %s]\n", name)
			fn()
			fmt.Printf("  [logged: ← %s]\n", name)
		}
	}
}

// cached wraps fn with a cache stub.
// Curried: cached(size) returns a func(func()) func().
func cached(size int) func(func()) func() {
	return func(fn func()) func() {
		return func() {
			fmt.Printf("  [cached: size=%d]\n", size)
			fn()
		}
	}
}

// normalFunc has no decorators.
func normalFunc() {
	fmt.Println("  Normal function called")
}

// timedFunc is wrapped by timed — it will log elapsed time after the body.
@timed
func timedFunc() {
	fmt.Println("  Timed function called")
}

// loggedFunc is wrapped by logged — logs entry and exit.
@logged("myFunction")
func loggedFunc() {
	fmt.Println("  Logged function called")
}

// multiDecoratedFunc is wrapped by all three, outermost first:
//   timed( logged("multiDecorated")( cached(100)( func(){body} ) ) )()
@timed
@logged("multiDecorated")
@cached(100)
func multiDecoratedFunc() {
	fmt.Println("  Multi-decorated function called")
}

func main() {
	fmt.Println("=== Testing Go Decorator Syntax ===\n")

	fmt.Println("1. Normal function:")
	normalFunc()

	fmt.Println("\n2. @timed — wraps before AND after:")
	timedFunc()

	fmt.Println("\n3. @logged(\"myFunction\") — wraps with entry/exit log:")
	loggedFunc()

	fmt.Println("\n4. @timed @logged @cached — three layers, outermost=timed:")
	multiDecoratedFunc()
}
