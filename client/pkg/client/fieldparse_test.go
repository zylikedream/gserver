package client

import (
	"fmt"
	"testing"

	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestParseFieldValueScalarKinds:一张表覆盖 String / Int32 / Int64 / Enum 四种标量 kind 的解析结果。
// 为什么需要:这四格都走 ParseFieldValue 的类型分发,某个 kind 一旦走错分支,
// 压测脚本传进来的参数会静默变成零值,不会有任何报错。
func TestParseFieldValueScalarKinds(t *testing.T) {
	tests := []struct {
		name string
		in   string
		kind protoreflect.Kind
		want string
	}{
		{name: "string", in: "hello", kind: protoreflect.StringKind, want: "hello"},
		{name: "int32", in: "42", kind: protoreflect.Int32Kind, want: "42"},
		{name: "int64", in: "1234567890123", kind: protoreflect.Int64Kind, want: "1234567890123"},
		{name: "enum", in: "2", kind: protoreflect.EnumKind, want: "2"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			v, err := ParseFieldValue(tt.in, FieldInfo{Kind: tt.kind})
			if err != nil {
				t.Fatal(err)
			}
			var got string
			switch tt.kind {
			case protoreflect.StringKind:
				got = v.String()
			case protoreflect.EnumKind:
				got = fmt.Sprint(v.Enum())
			default:
				got = fmt.Sprint(v.Int())
			}
			if got != tt.want {
				t.Errorf("ParseFieldValue(%q, %v) = %q, want %q", tt.in, tt.kind, got, tt.want)
			}
		})
	}
}

// TestParseFieldValueBool:单独一格,覆盖 BoolKind 的四种真值写法("1"/"true"/"True"/"TRUE")与假值 "0"。
// 为什么需要:机器人参数里 true 的大小写写法不统一,一旦某个写法被解析成 false,
// 整场压测会静默地全部按"关"来跑。
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

// TestParseFieldValueUnsupported:MessageKind 无法从命令行文本构造,必须显式报错而不是返回空值。
// 为什么需要:解析失败若被吞掉,压测端会把"不支持的字段"当成零值发出去,服务端收到的是错包而不是错误。
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
