package client

import (
	"fmt"
	"strconv"
	"strings"

	"google.golang.org/protobuf/reflect/protoreflect"
)

type FieldInfo struct {
	Name     string
	Kind     protoreflect.Kind
	Repeated bool
}

func ParseFieldValue(s string, fi FieldInfo) (protoreflect.Value, error) {
	switch fi.Kind {
	case protoreflect.StringKind:
		return protoreflect.ValueOfString(s), nil
	case protoreflect.Int32Kind, protoreflect.Sint32Kind, protoreflect.Sfixed32Kind:
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfInt32(int32(n)), nil
	case protoreflect.Int64Kind, protoreflect.Sint64Kind, protoreflect.Sfixed64Kind:
		n, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfInt64(n), nil
	case protoreflect.Uint32Kind, protoreflect.Fixed32Kind:
		n, err := strconv.ParseUint(s, 10, 32)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfUint32(uint32(n)), nil
	case protoreflect.Uint64Kind, protoreflect.Fixed64Kind:
		n, err := strconv.ParseUint(s, 10, 64)
		if err != nil {
			return protoreflect.Value{}, err
		}
		return protoreflect.ValueOfUint64(n), nil
	case protoreflect.BoolKind:
		return protoreflect.ValueOfBool(s == "1" || strings.EqualFold(s, "true")), nil
	case protoreflect.EnumKind:
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return protoreflect.Value{}, fmt.Errorf("enum value must be numeric: %s", s)
		}
		return protoreflect.ValueOfEnum(protoreflect.EnumNumber(n)), nil
	default:
		return protoreflect.Value{}, fmt.Errorf("unsupported kind %v", fi.Kind)
	}
}

func ExpandCommaSeparated(args []string) []string {
	var out []string
	for _, a := range args {
		if strings.Contains(a, ",") {
			for _, p := range strings.Split(a, ",") {
				p = strings.TrimSpace(p)
				if p != "" {
					out = append(out, p)
				}
			}
		} else {
			a = strings.TrimSpace(a)
			if a != "" {
				out = append(out, a)
			}
		}
	}
	return out
}
