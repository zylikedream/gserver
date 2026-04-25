package ets

import "sync"

type etsStruct struct {
	m    sync.Map
	name string
}

func NewETS(name string) *etsStruct {
	return &etsStruct{
		name: name,
	}
}

func (e *etsStruct) Set(key any, value any) {
	e.m.Store(key, value)
}

func (e *etsStruct) Get(key any, def ...any) any {
	v, ok := e.m.Load(key)
	if !ok {
		if len(def) > 0 {
			return def[0]
		}
		return nil
	}
	return v
}
