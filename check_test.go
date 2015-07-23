package main

import (
	"reflect"
	"testing"
)

func Test_Vendo_Tree_Insert(test *testing.T) {
	t := Tree{}
	t.Put("foo/bar/baz")
	t.Put("foo")
	t.Put("foo/bang")
	t.Put("foo/bar/baz/boo")
	t.Put("fee")
	expected := Tree{
		"foo": Tree{
			"bar": Tree{
				"baz": Tree{
					"boo": Tree{},
				},
			},
			"bang": Tree{},
		},
		"fee": Tree{},
	}
	if !reflect.DeepEqual(t, expected) {
		test.Errorf("expected:\n%v\ngot:\n%v", expected, t)
	}
}
