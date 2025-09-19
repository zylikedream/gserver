/*
 * @Author: your name
 * @Date: 2021-11-04 14:34:02
 * @LastEditTime: 2021-11-04 15:13:50
 * @LastEditors: Please set LastEditors
 * @Description: In User Settings Edit
 * @FilePath: /components/gxynet/peer/peer.go
 */
package peer

import (
	"context"

	"gserver/core/gxynet/endpoint"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/gcfg"
)

const (
	PEER_TCP_SERVER    = "peer.tcp_server"
	PEER_TCP_CONNECTOR = "peer.tcp_connector"
)

type Peer interface {
	Start(ctx context.Context, h endpoint.EventHandler) error
	Stop(ctx context.Context) error
}

func NewPeer(t string, c *gcfg.Config) (Peer, error) {
	switch "peer." + t {
	case PEER_TCP_SERVER:
		return newTcpServer(c)
	case PEER_TCP_CONNECTOR:
		return newTcpConnector(c)
	}
	return nil, gerror.Newf("peer type: %s not found", t)
}
