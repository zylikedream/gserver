package packet

import (
	"bytes"
	"encoding/binary"

	"gserver/core/gxynet/message"
	"gserver/util"

	"github.com/gogf/gf/v2/encoding/gbinary"
	"github.com/gogf/gf/v2/os/gcfg"
	"github.com/gogf/gf/v2/util/gconv"
	"github.com/pkg/errors"
)

// length + type + id + payload
type ltpv struct {
	byteOrder binary.ByteOrder `toml:"byte_order"`
	conf      *ltpvConfig
}

type ltpvConfig struct {
	SizeLength int    `toml:"size_length"`
	MaxSize    int    `toml:"max_size"`
	TypeLength int    `toml:"type_length"`
	PathLength int    `toml:"path_length"`
	ByteOrder  string `toml:"byte_order"`
}

func newLtpv(c *gcfg.Config) (*ltpv, error) {
	l := &ltpv{}
	conf := &ltpvConfig{}
	if err := util.CfgUnmarshalKey(ctx, c, l.Type(), conf); err != nil {
		return nil, errors.WithStack(err)
	}
	if conf.ByteOrder == "little" {
		l.byteOrder = binary.LittleEndian
	} else {
		l.byteOrder = binary.BigEndian
	}
	l.conf = conf
	return l, nil
}

func (l *ltpv) decodeBody(payLoad []byte) (*message.Message, error) {
	msg := &message.Message{}
	// 消息类型+消息id+消息内容
	if tp, err := uintDecode(payLoad[:l.conf.TypeLength], l.byteOrder); err != nil {
		return nil, errors.WithStack(err)
	} else {
		msg.Type = gconv.Uint16(tp)
	}
	payLoad = payLoad[l.conf.TypeLength:]
	// 消息path
	if pathLen, err := uintDecode(payLoad[:l.conf.PathLength], l.byteOrder); err != nil {
		return nil, errors.WithStack(err)
	} else {
		path := gbinary.DecodeToString(payLoad[l.conf.PathLength : l.conf.PathLength+int(pathLen)])
		msg.Path = path
		payLoad = payLoad[l.conf.PathLength+int(pathLen):]
	}

	msg.Payload = payLoad

	return msg, nil
}

func (l *ltpv) ByteOrder() binary.ByteOrder {
	return l.byteOrder
}

func (l *ltpv) Decode(data []byte) (uint64, *message.Message, error) {
	if len(data) < l.conf.SizeLength {
		return 0, nil, ErrPkgHeadNotEnough
	}
	dataSize, err := uintDecode(data[:l.conf.SizeLength], l.byteOrder)
	if err != nil {
		return 0, nil, err
	}
	if dataSize > uint64(l.conf.MaxSize) {
		return 0, nil, errors.WithStack(errors.Errorf("packet too big, %d(%d)", dataSize, l.conf.MaxSize))
	}
	data = data[l.conf.SizeLength:]
	if len(data) < int(dataSize) {
		return 0, nil, errors.WithStack(ErrPkgBodyNotEnough)
	}
	msg, err := l.decodeBody(data[:dataSize])
	return dataSize + uint64(l.conf.SizeLength), msg, err
}

func (l *ltpv) Encode(m *message.Message) ([]byte, error) {
	payload := &bytes.Buffer{}
	// 消息长度
	// 消息类型+消息path+消息内容
	if err := binary.Write(payload, l.byteOrder, convertUint(uint64(m.Type), l.conf.TypeLength)); err != nil {
		return nil, errors.WithStack(err)
	}
	pathLen := len(m.Path)
	if err := binary.Write(payload, l.byteOrder, convertUint(gconv.Uint64(pathLen), l.conf.PathLength)); err != nil {
		return nil, errors.WithStack(err)
	}
	if _, err := payload.Write(gconv.Bytes(m.Path)); err != nil {
		return nil, errors.WithStack(err)
	}
	if _, err := payload.Write(m.Payload); err != nil {
		return nil, errors.WithStack(err)
	}
	m.Payload = payload.Bytes()
	buf := &bytes.Buffer{}
	payloadLen := len(m.Payload)
	if err := binary.Write(buf, l.byteOrder, convertUint(uint64(payloadLen), l.conf.SizeLength)); err != nil {
		return nil, errors.WithStack(err)
	}
	if _, err := buf.Write(m.Payload); err != nil {
		return nil, errors.WithStack(err)
	}
	return buf.Bytes(), nil
}

func (l *ltpv) Type() string {
	return PACKET_LTPV
}
