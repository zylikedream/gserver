package client

import (
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

func TestParseFieldValueString(t *testing.T) {
	v, err := ParseFieldValue("hello", FieldInfo{Kind: protoreflect.StringKind})
	if err != nil {
		t.Fatal(err)
	}
	if v.String() != "hello" {
		t.Errorf("got %q, want %q", v.String(), "hello")
	}
}

func TestParseFieldValueInt32(t *testing.T) {
	v, err := ParseFieldValue("42", FieldInfo{Kind: protoreflect.Int32Kind})
	if err != nil {
		t.Fatal(err)
	}
	if v.Int() != 42 {
		t.Errorf("got %d, want %d", v.Int(), 42)
	}
}

func TestParseFieldValueInt64(t *testing.T) {
	v, err := ParseFieldValue("1234567890123", FieldInfo{Kind: protoreflect.Int64Kind})
	if err != nil {
		t.Fatal(err)
	}
	if v.Int() != 1234567890123 {
		t.Errorf("got %d, want %d", v.Int(), 1234567890123)
	}
}

func TestParseFieldValueBool(t *testing.T) {
	for _, s := range []string{"1", "true", "True", "TRUE"} {
		v, err := ParseFieldValue(s, FieldInfo{Kind: protoreflect.BoolKind})
		if err != nil {
			t.Fatalf("ParseFieldValue(%q): %v", s, err)
		}
		if !v.Bool() {
			t.Errorf("ParseFieldValue(%q) = false, want true", s)
		}
	}
	v, err := ParseFieldValue("0", FieldInfo{Kind: protoreflect.BoolKind})
	if err != nil {
		t.Fatal(err)
	}
	if v.Bool() {
		t.Error("ParseFieldValue(\"0\") = true, want false")
	}
}

func TestParseFieldValueEnum(t *testing.T) {
	v, err := ParseFieldValue("2", FieldInfo{Kind: protoreflect.EnumKind})
	if err != nil {
		t.Fatal(err)
	}
	if v.Enum() != 2 {
		t.Errorf("got %d, want %d", v.Enum(), 2)
	}
}

func TestParseFieldValueUnsupported(t *testing.T) {
	_, err := ParseFieldValue("x", FieldInfo{Kind: protoreflect.MessageKind})
	if err == nil {
		t.Error("expected error for MessageKind")
	}
}

func TestExpandCommaSeparated(t *testing.T) {
	tests := []struct {
		input []string
		want  []string
	}{
		{[]string{"a", "b"}, []string{"a", "b"}},
		{[]string{"a,b", "c"}, []string{"a", "b", "c"}},
		{[]string{"1,2,3"}, []string{"1", "2", "3"}},
		{[]string{""}, []string{}},
		{[]string{}, []string{}},
	}
	for _, tc := range tests {
		got := ExpandCommaSeparated(tc.input)
		if len(got) != len(tc.want) {
			t.Errorf("ExpandCommaSeparated(%v) = %v, want %v", tc.input, got, tc.want)
			continue
		}
		for i := range got {
			if got[i] != tc.want[i] {
				t.Errorf("ExpandCommaSeparated(%v) = %v, want %v", tc.input, got, tc.want)
				break
			}
		}
	}
}
