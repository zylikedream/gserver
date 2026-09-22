package gxyactor

import (
	"testing"

	"gserver/protocol/pb"

	"ergo.services/ergo/gen"
)

// wire 表示里 Name 装的是**注册名**,不是实例标识。
//
// 跨节点只能按名寻址:运行时的进程标识含"哪一代实例",对端重启后即失效。
// 这条契约两端(writer/reader)必须一致,否则跨节点引用会静默失效。
func TestPidRoundTripLocal(t *testing.T) {
	origin := pidFromLocal(gen.PID{Node: "node-a", ID: 42, Creation: 1700000000})

	got := PBToPid(PidToPB(origin))

	if !PidEqual(got, origin) {
		t.Fatalf("本机引用往返后不等: got=%v want=%v", got, origin)
	}
	if got.local != origin.local {
		t.Fatalf("本机标识必须逐字段保留: got=%+v want=%+v", got.local, origin.local)
	}
}

// 跨节点引用按"节点 + 注册名"往返。
func TestPidRoundTripRemote(t *testing.T) {
	origin := pidFromRemote("node-b", actorKey{kind: "role", id: "7"}.name())

	got := PBToPid(PidToPB(origin))

	if !PidEqual(got, origin) {
		t.Fatalf("跨节点引用往返后不等: got=%v want=%v", got, origin)
	}
	if got.Name() != "role/7" {
		t.Fatalf("名字 = %q, want role/7", got.Name())
	}
	if got.Node() != "node-b" {
		t.Fatalf("节点 = %q, want node-b", got.Node())
	}
}

// 判别必须依据"是否带完整的本机进程标识",而不是单看某一个字段:
// 缺任一部分都退回按名寻址,绝不拼出一个 ID 为零的本机标识。
func TestPBToPidNeedsFullLocalIdentity(t *testing.T) {
	cases := map[string]*pb.ActorPid{
		"只有 creation": {Address: "node-a", Creation: 1700000000},
		"只有 pid":      {Address: "node-a", Pid: 42},
		"缺 address":   {Pid: 42, Creation: 1700000000},
		"只有 address":  {Address: "node-b"},
		"全空":          {},
	}
	for name, in := range cases {
		if got := PBToPid(in); !got.IsZero() {
			t.Fatalf("%s: 应还原为零值引用,实际 %v", name, got)
		}
	}
}

// 本机标识齐备时按标识寻址,即使同时带了名字。
func TestPBToPidPrefersLocalIdentity(t *testing.T) {
	in := &pb.ActorPid{Address: "node-a", Name: "role/7", Pid: 42, Creation: 1700000000}

	got := PBToPid(in)

	if got.local.Node == "" {
		t.Fatal("带完整本机标识时必须按标识寻址")
	}
	if got.local.ID != 42 || got.local.Creation != 1700000000 {
		t.Fatalf("本机标识 = %+v, want {ID:42 Creation:1700000000}", got.local)
	}
}
