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

func (j *JsonMessage) Decode(msg interface{}, data []byte) error {
	return json.Unmarshal(data, msg)
}

func (j *JsonMessage) Encode(msg interface{}) ([]byte, error) {
	data, err := json.Marshal(msg)
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return data, nil
}
