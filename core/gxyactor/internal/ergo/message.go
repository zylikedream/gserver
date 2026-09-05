package ergo

import (
	"errors"
	"fmt"
	"reflect"
	"sync"

	"google.golang.org/protobuf/proto"
)

// GServerEnvelope is the only wire value used for remote business messages.
// Type is a stable application-level identifier and Data is proto.Marshal output.
type GServerEnvelope struct {
	Type string
	Data []byte
}

var (
	ErrUnknownMessageType    = errors.New("unknown gserver message type")
	ErrMissingRegistration   = errors.New("gserver message type is not registered")
	ErrDuplicateRegistration = errors.New("gserver message type registration already exists")
	ErrMalformedPayload      = errors.New("malformed gserver protobuf payload")
)

type messageRegistration struct {
	newMessage func() proto.Message
	typ        reflect.Type
}

// MessageRegistry maps stable wire identifiers to protobuf constructors.
type MessageRegistry struct {
	mu     sync.RWMutex
	byName map[string]messageRegistration
	byType map[reflect.Type]string
}

func NewMessageRegistry() *MessageRegistry {
	return &MessageRegistry{byName: make(map[string]messageRegistration), byType: make(map[reflect.Type]string)}
}

func (r *MessageRegistry) Register(name string, constructor func() proto.Message) error {
	if r == nil || name == "" || constructor == nil {
		return fmt.Errorf("%w: %q", ErrMissingRegistration, name)
	}
	message := constructor()
	if message == nil {
		return fmt.Errorf("%w: %q constructor returned nil", ErrMissingRegistration, name)
	}
	typ := reflect.TypeOf(message)
	r.mu.Lock()
	defer r.mu.Unlock()
	if previous, exists := r.byName[name]; exists && previous.typ != typ {
		return fmt.Errorf("%w: %q already registered", ErrDuplicateRegistration, name)
	}
	if previous, exists := r.byType[typ]; exists && previous != name {
		return fmt.Errorf("%w: %s already registered as %q", ErrDuplicateRegistration, typ, previous)
	}
	r.byName[name] = messageRegistration{newMessage: constructor, typ: typ}
	r.byType[typ] = name
	return nil
}

func (r *MessageRegistry) Encode(message proto.Message) (GServerEnvelope, error) {
	if r == nil || message == nil {
		return GServerEnvelope{}, fmt.Errorf("%w: nil message", ErrMissingRegistration)
	}
	typ := reflect.TypeOf(message)
	r.mu.RLock()
	name, exists := r.byType[typ]
	r.mu.RUnlock()
	if !exists {
		return GServerEnvelope{}, fmt.Errorf("%w: %s", ErrMissingRegistration, typ)
	}
	data, err := proto.Marshal(message)
	if err != nil {
		return GServerEnvelope{}, fmt.Errorf("%w: %v", ErrMalformedPayload, err)
	}
	return GServerEnvelope{Type: name, Data: data}, nil
}

func (r *MessageRegistry) Decode(envelope GServerEnvelope) (proto.Message, error) {
	if r == nil {
		return nil, fmt.Errorf("%w: registry is nil", ErrMissingRegistration)
	}
	r.mu.RLock()
	registration, exists := r.byName[envelope.Type]
	r.mu.RUnlock()
	if !exists {
		return nil, fmt.Errorf("%w: %q", ErrUnknownMessageType, envelope.Type)
	}
	message := registration.newMessage()
	if message == nil {
		return nil, fmt.Errorf("%w: %q constructor returned nil", ErrMissingRegistration, envelope.Type)
	}
	if err := proto.Unmarshal(envelope.Data, message); err != nil {
		return nil, fmt.Errorf("%w: %q: %v", ErrMalformedPayload, envelope.Type, err)
	}
	return message, nil
}

func (r *MessageRegistry) Registered(name string) bool {
	if r == nil {
		return false
	}
	r.mu.RLock()
	_, ok := r.byName[name]
	r.mu.RUnlock()
	return ok
}
