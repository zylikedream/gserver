package gxyutil

import (
	"reflect"

	"github.com/gookit/goutil/reflects"
)

func GetName[T any]() string {
	return GetTypeName(reflect.TypeFor[T]())
}

func GetObjectName(obj interface{}) string {
	return GetTypeName(reflect.TypeOf(obj))
}

func GetTypeName(t reflect.Type) string {
	return reflects.TypeReal(t).Name()
}

func NewObject(t reflect.Type) interface{} {
	return reflect.New(reflects.TypeReal(t)).Interface()
}

func TypeReal(obj any) reflect.Type {
	return reflects.TypeReal(reflect.TypeOf(obj))
}
