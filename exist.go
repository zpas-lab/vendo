package main

import (
	"fmt"
	"os"
)

// Exist is utility struct for easy checking existence of multiple files and
// directories. Example usage:
//
//	// Verify that both file "foo" and dir "bar/" exist.
//	err := Exist{}.File("foo").Dir("bar")
//	if err != nil {
//		panic("bad filesystem structure: " + err.Error())
//	}
type Exist struct{ Err error }

func (e Exist) Dir(path string) Exist {
	if e.Err != nil {
		return e
	}
	info, err := os.Stat(path)
	if err != nil {
		return Exist{err}
	}
	if !info.IsDir() {
		return Exist{fmt.Errorf("not a directory: %s", path)}
	}
	return Exist{}
}

func (e Exist) File(path string) Exist {
	if e.Err != nil {
		return e
	}
	info, err := os.Stat(path)
	if err != nil {
		return Exist{err}
	}
	if info.IsDir() {
		return Exist{fmt.Errorf("not a file: %s", path)}
	}
	return Exist{}
}
