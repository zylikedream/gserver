package util

import (
	"context"
	"fmt"
	"reflect"
	"runtime"
	"unicode"
	"unicode/utf8"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/text/gstr"
	"github.com/pkg/errors"
	"github.com/smallnest/rpcx/share"
)

const METHOD_PREFIX_ANY = ""

func GetServicePath(service string, method string) string {
	return service + "." + method
}

func GetTokenFromCtx(ctx context.Context) string {
	meta := ctx.Value(share.ReqMetaDataKey).(map[string]string)
	return meta["token"]
}

type MethodMeta struct {
	Method  reflect.Method
	ArgType reflect.Type
}

func (m *MethodMeta) Call(ctx context.Context, rcver any, argv any) (reply any, err error) {
	defer func() {
		if r := recover(); r != nil {
			buf := make([]byte, 4096)
			n := runtime.Stack(buf, false)
			buf = buf[:n]

			err = errors.New(fmt.Sprintf("error:%v, method: %s, argv: %+v, stack: %s",
				r, m.Method.Name, argv, buf))
		}
	}()

	function := m.Method.Func
	// Invoke the method, providing a new value for the reply.
	returnValues := function.Call([]reflect.Value{reflect.ValueOf(rcver), reflect.ValueOf(ctx),
		reflect.ValueOf(argv)})
	// The return value for the method is an error.
	var errInter any
	if len(returnValues) == 1 {
		errInter = returnValues[0].Interface()
	} else {
		reply = returnValues[0].Interface()
		errInter = returnValues[1].Interface()
	}

	if errInter != nil {
		err = errInter.(error)
		return
	}
	return
}

var typeOfContext = reflect.TypeFor[context.Context]()
var typeOfError = reflect.TypeFor[error]()

func GetSuitableMethods(typ reflect.Type, methodPrefix string) []*MethodMeta {
	methods := []*MethodMeta{}
	for i := 0; i < typ.NumMethod(); i++ {
		method := typ.Method(i)
		mtype := method.Type

		// method must be exported
		if method.PkgPath != "" {
			continue
		}
		// methods arg must be receiver, context, *args
		if mtype.NumIn() != 3 {
			continue
		}

		ctxType := mtype.In(1)
		if !ctxType.Implements(typeOfContext) {
			continue
		}

		argType := mtype.In(2)
		if !isExportedOrBuildinType(argType) {
			continue
		}

		if !gstr.HasPrefix(method.Name, methodPrefix) {
			continue
		}
		meta := &MethodMeta{Method: method, ArgType: argType}
		// method needs one error out
		if mtype.NumOut() == 1 {
			if returnType := mtype.Out(0); returnType != typeOfError {
				continue
			}
		} else if mtype.NumOut() == 2 {
			if returnType := mtype.Out(1); returnType != typeOfError {
				continue
			}
		} else {
			continue
		}
		methods = append(methods, meta)
	}
	return methods
}

func isExportedOrBuildinType(t reflect.Type) bool {
	for t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	return isExported(t.Name()) || t.PkgPath() == ""
}

func isExported(name string) bool {
	rune, _ := utf8.DecodeRuneInString(name)
	return unicode.IsUpper(rune)
}

type MsgHandler struct {
	methodMap     map[string]MsgMethod
	metas         []*MethodMeta
	handlerPrefix string
}

type MsgMethod struct {
	meta    *MethodMeta
	handler any
}

func (m *MsgMethod) GetMeta() *MethodMeta {
	return m.meta
}

func NewMsgHandler(handlerPrefix ...string) *MsgHandler {
	msgHandler := &MsgHandler{
		methodMap:     make(map[string]MsgMethod),
		handlerPrefix: "",
	}
	if len(handlerPrefix) > 0 {
		msgHandler.handlerPrefix = handlerPrefix[0]
	}
	return msgHandler
}

func (m *MsgHandler) AddHandler(handler any) {
	m.metas = GetSuitableMethods(reflect.TypeOf(handler), m.handlerPrefix)
	for _, meta := range m.metas {
		msgMethod := MsgMethod{
			meta:    meta,
			handler: handler,
		}

		m.methodMap[meta.ArgType.Name()] = msgMethod
	}
}

func (m *MsgHandler) CallWithMsg(ctx context.Context, msg any) (any, error) {
	method, ok := m.methodMap[reflect.TypeOf(msg).Name()]
	if !ok {
		return nil, gerror.Newf("no method handler (%s)", reflect.TypeOf(msg).Name())
	}
	return method.meta.Call(ctx, method.handler, msg)
}

func (m *MsgHandler) GetMethodMeta(msg any) *MethodMeta {
	method, ok := m.methodMap[reflect.TypeOf(msg).Name()]
	if !ok {
		return nil
	}
	return method.GetMeta()
}

func (m *MsgHandler) GetHandlers() map[string]MsgMethod {
	return m.methodMap
}
