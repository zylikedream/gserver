package codec

import (
	"gserver/protocol/pb"
	"gserver/util"
	"testing"
)

func TestProtobuf(t *testing.T) {
	msg := &pb.ReqAccountLogin{
		AccountUid: "oiewr",
		Client:     "",
	}

	// 或者使用我们的工具函数（如果需要更自定义的格式）
	t.Logf("%s", util.FormatObject(msg))
}
