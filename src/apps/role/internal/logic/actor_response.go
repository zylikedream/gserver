package logic

import (
	"github.com/cockroachdb/errors"
	"google.golang.org/protobuf/proto"
)

// asResponse 把 actor 调用的返回值断言为期望的响应类型。
//
// 调用返回 any:业务失败已由门面还原成 error(见 core/gxyactor.Actor.Call),
// 因此走到这里说明调用成功,载荷理应是期望的响应类型。但直接写 rsp.(*pb.RspXxx)
// 一旦类型不符就是 panic,而 panic 会让整个 role actor 终止、玩家掉线——代价
// 远大于返回一个错误。
func asResponse[T proto.Message](rsp any) (T, error) {
	var zero T
	typed, ok := rsp.(T)
	if !ok {
		return zero, errors.Newf("actor 响应类型不符: 期望 %T, 实际 %T", zero, rsp)
	}
	return typed, nil
}
