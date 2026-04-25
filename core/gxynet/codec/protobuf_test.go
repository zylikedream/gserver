package codec

import (
	"gserver/core/gxyutil"
	"gserver/protocol/pb"
	"testing"
)

func TestProtobuf(t *testing.T) {
	msg := &pb.ReqAccountLogin{
		RoleId: 1,
		Client: "",
	}

	// 或者使用我们的工具函数（如果需要更自定义的格式）
	t.Logf("%s", gxyutil.FormatObject(msg))
}
