package main

import (
	"errors"
	"fmt"
)

func divide(a, b int) (int, error) {
	if b == 0 {
		return 0, errors.New("divide by zero")
	}
	return a / b, nil
}

func splitAdd(a, b int) (int, int, error) {
	if b == 0 {
		return 0, 0, errors.New("split by zero")
	}
	return a / b, a % b, nil
}

func safeDivide(a, b int) (int, error) {
	q := divide(a, b)?
	return q, nil
}

func safeSplit(a, b int) (int, int, error) {
	quot := splitAdd(a, b)?[0]
	rem := splitAdd(a, b)?[1]
	return quot, rem, nil
}

func main() {
	q, err := safeDivide(10, 2)
	fmt.Println("10/2 =", q, err)

	q2, err2 := safeDivide(10, 0)
	fmt.Println("10/0 =", q2, err2)

	quot, rem, err3 := safeSplit(17, 5)
	fmt.Println("17 split 5 =", quot, rem, err3)

	quot2, rem2, err4 := safeSplit(17, 0)
	fmt.Println("17 split 0 =", quot2, rem2, err4)
}
