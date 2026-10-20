package gxyredis

import (
	"context"
	"net"
	"testing"
	"time"
)

// blackhole 是接受连接但从不回包的 TCP 服务:用来观察"命令执行阶段"的超时,
// 连接池等待阶段本来就会看 ctx,测不出选项的差别。
func blackhole(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			t.Cleanup(func() { _ = conn.Close() })
			// 不读不写:让对端一直等在读上
		}
	}()
	return ln.Addr().String()
}

// TestClientRespectsContextDeadline 固定一条选项决策:客户端必须开启
// ContextTimeoutEnabled。否则调用方传的 ctx 超时/取消会被静默换成
// context.Background(),命令只能等读超时(默认 3s),取消也停不下来。
func TestClientRespectsContextDeadline(t *testing.T) {
	cli := newClient(&redisConfig{Addr: blackhole(t)})
	t.Cleanup(func() { _ = cli.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := cli.Set(ctx, "k", "v", 0).Err()
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("对端不回包时必须失败")
	}
	if elapsed > time.Second {
		t.Fatalf("50ms 的 ctx deadline 没有约束命令执行(等了 %s):ContextTimeoutEnabled 未生效", elapsed)
	}
}
