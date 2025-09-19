package message

type RawMessage struct {
}

func newRawMessage() (*RawMessage, error) {
	return &RawMessage{}, nil
}

func (j *RawMessage) Decode(msg interface{}, data []byte) error {
	return nil
}

func (j *RawMessage) Encode(msg interface{}) ([]byte, error) {
	return msg.([]byte), nil
}
