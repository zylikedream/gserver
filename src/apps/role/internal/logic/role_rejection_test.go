package logic

import (
	"testing"

	"github.com/cockroachdb/errors"
)

// 声明即入册:凡是经 clientRejection 声明的错误都必须被判为预期拒绝,
// 且被 Wrap 包裹后仍然识别——客户端协议错误的降级依赖这条判定。
func TestClientRejection_RegisteredSentinels(t *testing.T) {
	registered := []error{
		ErrChatNotFriend,
		ErrChatCooldown,
		ErrChatMsgEmpty,
		ErrChatMsgTooLong,
		ErrFlowerLocked,
		ErrPlotNotReady,
		ErrStealDailyFull,
		ErrMainTaskFinished,
		ErrOrderMilestoneClaimed,
		ErrGoodNotEnough,
		ErrMailNotFound,
	}
	for _, err := range registered {
		if !isClientRejection(err) {
			t.Fatalf("哨兵 %v 未判为预期拒绝:声明处漏了 clientRejection?", err)
		}
		if !isClientRejection(errors.Wrap(err, "req handler")) {
			t.Fatalf("被包裹的哨兵 %v 未判为预期拒绝", err)
		}
	}
}

// 服务端自身的失败不得被降级:它必须以 error + 栈暴露出来。
func TestClientRejection_ServerSideErrorsAreNotRejections(t *testing.T) {
	cases := []error{
		nil,
		ErrGoodConfigNotFound,
		errors.New("some internal failure"),
		errors.Newf("db timeout after %dms", 500),
	}
	for _, err := range cases {
		if isClientRejection(err) {
			t.Fatalf("%v 被判为预期拒绝,但它不是客户端可预期的问题", err)
		}
	}
}

// 声明即入册:登记表非空,且每个登记项都能被判定命中。
func TestClientRejection_TableIsPopulated(t *testing.T) {
	if len(clientRejections) == 0 {
		t.Fatal("登记表为空:clientRejection 未生效")
	}
	for _, rejection := range clientRejections {
		if !isClientRejection(rejection) {
			t.Fatalf("登记项 %v 无法被判定命中", rejection)
		}
	}
}
