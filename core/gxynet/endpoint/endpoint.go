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
)

type Endpoint interface {
	SendData(data []byte, path string) error
	SendMsg(msg any) error
	Conn() net.Conn
	Close()
	GetData() interface{}
	SetData(interface{})
}
