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
	// Test case 1: nil safety with simple field access — whole chain is
	// nil, should yield the zero value ("") not panic.
	var user *User
	city := user?.Address?.City
	fmt.Printf("City: %q (want \"\")\n", city)

	// Test case 2: non-nil access — full chain resolves.
	user2 := &User{
		Name: "John",
		Address: &Address{
			City: "NYC",
			Street: &Street{
				Name: "Broadway",
			},
		},
	}
	street := user2?.Address?.Street?.Name
	fmt.Printf("Street: %q (want \"Broadway\")\n", street)

	// Test case 3: partial nil chain — user3 non-nil but Address nil.
	user3 := &User{
		Name:    "Jane",
		Address: nil,
	}
	city3 := user3?.Address?.City
	fmt.Printf("City3: %q (want \"\")\n", city3)

	// Test case 4: mixed ?. and . — user4 non-nil, Address non-nil,
	// Street nil.
	user4 := &User{
		Name: "Bob",
		Address: &Address{
			City:   "LA",
			Street: nil,
		},
	}
	name4 := user4?.Address?.Street?.Name
	fmt.Printf("Name4: %q (want \"\")\n", name4)
}
