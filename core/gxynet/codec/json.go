package codec

import (
	"encoding/json"

	"github.com/cockroachdb/errors"
)

type JsonMessage struct {
}

func newJsonMessage() (*JsonMessage, error) {
	return &JsonMessage{}, nil
}

func (j *JsonMessage) Decode(msg any, data []byte) error {
	return json.Unmarshal(data, msg)
}

func (j *JsonMessage) Encode(msg any) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return data, nil
}
