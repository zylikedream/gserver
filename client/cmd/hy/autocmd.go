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

	// Multi-word domain exceptions (2+ PascalCase words forming a single domain)
	multiWord := []struct{ pascal, lower string }{
		{"ResidentOrder", "residentorder"},
		{"MainTask", "maintask"},
	}
	for _, mw := range multiWord {
		if strings.HasPrefix(rest, mw.pascal) {
			action := rest[len(mw.pascal):]
			if action == "" {
				return mw.lower + ".info"
			}
			return mw.lower + "." + pascalToSnake(action)
		}
	}

	// Auto-detect: first PascalCase word is the domain,
	// remainder is the action in snake_case
	for i := 1; i < len(rest); i++ {
		if !unicode.IsUpper(rune(rest[i])) {
			continue
		}
		if unicode.IsUpper(rune(rest[i-1])) {
			// Consecutive uppercase — possible acronym
			if i+1 < len(rest) && !unicode.IsUpper(rune(rest[i+1])) {
				// i is first letter of new word, domain is before it
				domain := strings.ToLower(rest[:i])
				action := pascalToSnake(rest[i:])
				return domain + "." + action
			}
			continue
		}
		// Word boundary: "Mail" | "Claim"
		domain := strings.ToLower(rest[:i])
		action := pascalToSnake(rest[i:])
		return domain + "." + action
	}
	return strings.ToLower(rest) + ".info"
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
