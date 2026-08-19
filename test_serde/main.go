package main

import "fmt"

@serde
type Address struct {
	Street string
	City   string
	Zip    int
}

@serde
type User struct {
	Name    string
	Age     int
	Score   float64
	Active  bool
	Tags    []string
	Home    Address
	Backup  *Address
	Meta    map[string]string
}

func main() {
	u := User{
		Name:   "Ada",
		Age:    36,
		Score:  9.5,
		Active: true,
		Tags:   []string{"admin", "beta"},
		Home:   Address{Street: "1 Main St", City: "London", Zip: 1000},
		Backup: nil,
		Meta:   map[string]string{"role": "engineer"},
	}

	// --- JSON round trip ---
	je := NewJSONEncoder()
	if err := u.SerdeEncode(je); err != nil {
		panic(err)
	}
	jsonBytes := je.Bytes()
	fmt.Println("JSON:", string(jsonBytes))

	jd, err := NewJSONDecoder(jsonBytes)
	if err != nil {
		panic(err)
	}
	var u2 User
	if err := u2.SerdeDecode(jd); err != nil {
		panic(err)
	}
	fmt.Printf("JSON roundtrip match: %v\n", fmt.Sprintf("%+v", u) == fmt.Sprintf("%+v", u2))

	// --- Binary round trip, same struct, zero extra codegen ---
	be := NewBinaryEncoder()
	if err := u.SerdeEncode(be); err != nil {
		panic(err)
	}
	binBytes := be.Bytes()
	fmt.Println("Binary bytes:", len(binBytes))

	bd := NewBinaryDecoder(binBytes)
	var u3 User
	if err := u3.SerdeDecode(bd); err != nil {
		panic(err)
	}
	fmt.Printf("Binary roundtrip match: %v\n", fmt.Sprintf("%+v", u) == fmt.Sprintf("%+v", u3))

	// --- pointer field with a value, to exercise EncodeOptional(true) path ---
	u.Backup = &Address{Street: "2 Side St", City: "Leeds", Zip: 2000}
	je2 := NewJSONEncoder()
	if err := u.SerdeEncode(je2); err != nil {
		panic(err)
	}
	fmt.Println("JSON w/ backup:", je2.String())
	jd2, err := NewJSONDecoder(je2.Bytes())
	if err != nil {
		panic(err)
	}
	var u4 User
	if err := u4.SerdeDecode(jd2); err != nil {
		panic(err)
	}
	fmt.Printf("Backup roundtrip match: %v\n", fmt.Sprintf("%+v", *u.Backup) == fmt.Sprintf("%+v", *u4.Backup))
}
