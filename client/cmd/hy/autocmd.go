package main

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"hy_client/pkg/client"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

func registerAutoCommands() {
	client.RegisterMessages()

	client.RangeReqMessages(func(msgID, name string, md protoreflect.MessageDescriptor) bool {
		id, _ := strconv.Atoi(msgID)
		if id >= 10001 && id <= 10099 {
			return true
		}

		cmdName := deriveCommandName(name)
		if _, exists := commands[cmdName]; exists {
			return true
		}

		fields := parseableFields(md)
		typ := client.MessageTypeByID(msgID)
		if typ == nil {
			return true
		}

		var params []string
		for _, fi := range fields {
			if fi.Repeated {
				params = append(params, fi.Name+"...")
			} else {
				params = append(params, fi.Name)
			}
		}

		register(&Command{
			Name:   cmdName,
			Help:   fmt.Sprintf("[%s]", name),
			Params: params,
			Exec:   buildAutoExec(typ, fields),
		})
		return true
	})
}

func deriveCommandName(protoName string) string {
	rest := protoName[3:] // strip "Req"

	domainPrefixes := []struct{ pascal, lower string }{
		{"ResidentOrder", "residentorder"},
		{"MainTask", "maintask"},
		{"Friend", "friend"},
		{"Guild", "guild"},
		{"Chat", "chat"},
		{"Flower", "flower"},
		{"Plot", "plot"},
		{"Basic", "basic"},
		{"Bag", "bag"},
		{"GM", "gm"},
	}

	for _, dp := range domainPrefixes {
		if strings.HasPrefix(rest, dp.pascal) {
			action := rest[len(dp.pascal):]
			if action == "" {
				return dp.lower + ".info"
			}
			return dp.lower + "." + pascalToSnake(action)
		}
	}

	return strings.ToLower(rest[:1]) + "." + pascalToSnake(rest[1:])
}

func pascalToSnake(s string) string {
	var buf strings.Builder
	for i, r := range s {
		if i > 0 && unicode.IsUpper(r) {
			prev := rune(s[i-1])
			if !unicode.IsUpper(prev) {
				buf.WriteByte('_')
			} else if i+1 < len(s) && !unicode.IsUpper(rune(s[i+1])) {
				buf.WriteByte('_')
			}
		}
		buf.WriteRune(unicode.ToLower(r))
	}
	return buf.String()
}

func parseableFields(md protoreflect.MessageDescriptor) []client.FieldInfo {
	fields := md.Fields()
	var singular, repeated []client.FieldInfo

	for i := 0; i < fields.Len(); i++ {
		fd := fields.Get(i)

		if fd.Name() == "role_id" {
			continue
		}
		if fd.Kind() == protoreflect.MessageKind {
			continue
		}
		if fd.IsMap() {
			continue
		}

		fi := client.FieldInfo{
			Name:     string(fd.Name()),
			Kind:     fd.Kind(),
			Repeated: fd.IsList(),
		}

		if fd.IsList() {
			repeated = append(repeated, fi)
		} else {
			singular = append(singular, fi)
		}
	}

	return append(singular, repeated...)
}

func buildAutoExec(msgType reflect.Type, fields []client.FieldInfo) func(c *client.Client, args []string) error {
	return func(c *client.Client, args []string) error {
		msg := reflect.New(msgType).Interface().(proto.Message)
		mr := msg.ProtoReflect()

		nSingular := 0
		for _, f := range fields {
			if !f.Repeated {
				nSingular++
			}
		}

		if len(args) < nSingular {
			return fmt.Errorf("usage: %s", formatParamList(fields))
		}

		argIdx := 0
		for _, fi := range fields {
			if fi.Repeated {
				continue
			}
			if argIdx >= len(args) {
				break
			}
			fd := mr.Descriptor().Fields().ByName(protoreflect.Name(fi.Name))
			val, err := client.ParseFieldValue(args[argIdx], fi)
			if err != nil {
				return fmt.Errorf("invalid %s: %v", fi.Name, err)
			}
			mr.Set(fd, val)
			argIdx++
		}

		for _, fi := range fields {
			if !fi.Repeated {
				continue
			}
			if argIdx >= len(args) {
				break
			}
			fd := mr.Descriptor().Fields().ByName(protoreflect.Name(fi.Name))
			list := mr.Mutable(fd).List()
			expanded := client.ExpandCommaSeparated(args[argIdx:])
			for _, s := range expanded {
				val, err := client.ParseFieldValue(strings.TrimSpace(s), fi)
				if err != nil {
					return fmt.Errorf("invalid %s: %v", fi.Name, err)
				}
				list.Append(val)
			}
			argIdx = len(args)
		}

		return c.Request(msg)
	}
}

func formatParamList(fields []client.FieldInfo) string {
	var parts []string
	for _, fi := range fields {
		if fi.Repeated {
			parts = append(parts, fi.Name+"...")
		} else {
			parts = append(parts, fi.Name)
		}
	}
	return strings.Join(parts, " ")
}
