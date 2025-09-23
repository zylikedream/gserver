package message

const (
	MESSGE_TYPE_FIRST_PACKET = iota
	MESSAGE_TYPE_DATA_PACKET
)

type Message struct {
	Path    string
	Type    uint16
	Payload []byte
	Msg     any
}

func NewMessage(data []byte, path string) *Message {
	msg := &Message{
		Path:    path,
		Payload: data,
		Type:    MESSAGE_TYPE_DATA_PACKET,
	}
	return msg
}
