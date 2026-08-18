package main

import "fmt"

type User struct {
	Name    string
	Address *Address
}

type Address struct {
	City   string
	Street *Street
}

type Street struct {
	Name string
}

func main() {
	fmt.Println("=== Testing current behavior ===\n")
	
	// Test 1: Try to access nil - this should panic
	var user *User
	fmt.Println("Test 1: Accessing nil user (will panic)...")
	city := user.Address.City // This will panic
	fmt.Printf("city: %v\n", city)
}
