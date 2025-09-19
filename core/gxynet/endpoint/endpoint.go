/*
 * @Author: your name
 * @Date: 2021-10-19 17:41:17
 * @LastEditTime: 2021-11-04 17:05:48
 * @LastEditors: Please set LastEditors
 * @Description: In User Settings Edit
 * @FilePath: /components/gxynet/conn/endpoint.go
 */
package endpoint

import (
	"net"

	"gserver/core/gxynet/message"
)

type Endpoint interface {
	SendData(data []byte, path string, opts ...message.MessageOptionFunc) error
	SendMsg(msg any, opts ...message.MessageOptionFunc) error
	SendRaw(msg *message.Message, opts ...message.MessageOptionFunc) error
	Conn() net.Conn
	GetData() interface{}
	SetData(interface{})
}
