package logic

import "github.com/cockroachdb/errors"

// clientRejections 是已登记的"客户端可见的预期拒绝"哨兵,由 clientRejection 在
// 声明处填写。
var clientRejections []error

// clientRejection 声明一个客户端可见的预期拒绝哨兵:声明即登记。
//
// 这类错误是"客户端请求不满足前置条件"的正常结局——它以错误应答回给客户端,
// 既不需要重试,也不需要有人关注。最终处理点据此只记一条不带栈的摘要,而不是
// 一条 error + 完整栈(见 logClientProtocolError)。
//
// 声明即登记,所以不会出现"新增了校验错误却忘了登记"的漏项;反过来,只要用了
// 本构造函数,该错误就一定会被降级。**服务端自身的故障(配置缺失、依赖不可用、
// 数据不一致)不得用它声明**——那些必须继续以 error + 栈暴露出来。
func clientRejection(msg string) error {
	err := errors.New(msg)
	clientRejections = append(clientRejections, err)
	return err
}

// isClientRejection 判断错误是否属于预期内的客户端拒绝。
// 判定走 errors.Is,因此被 Wrap/WithStack 包裹过的哨兵同样能识别。
func isClientRejection(err error) bool {
	for _, rejection := range clientRejections {
		if errors.Is(err, rejection) {
			return true
		}
	}
	return false
}
