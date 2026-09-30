package logic

// 跨服务调用(role → friend)测试。
//
// friend 服务用真实 HTTP 服务器替身(core/gxyservice/gxyservicetest):
// 真 URL 拼接、真服务发现查询、真 HTTP 往返,只把「friend 在哪」换成测试进程内的假服务。
// 假服务按 friend 服务的路由与响应体说话,不是按 Go 函数签名——对端契约漂移时这里会红。

import (
	"context"
	"strings"
	"testing"

	"gserver/core/gxyhttp"
	"gserver/core/gxyservice/gxyservicetest"
	"gserver/protocol/pb"
	"gserver/src/apps/api"
	"gserver/src/pkg/deps"

	"github.com/alicebob/miniredis/v2"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/redis/go-redis/v9"
)

// fakeFriendData 镜像 friend 服务 /list 的响应体。
// 注意:friend 侧的 FriendData 没有 json tag,线上键就是大写字段名。
type fakeFriendData struct {
	PlayerID int64
	Friends  []fakeFriendEntry
	Incoming []fakeApplyEntry
	Outgoing []fakeApplyEntry
}

type fakeFriendEntry struct {
	PlayerID int64 `json:"player_id"`
	AddedAt  int64 `json:"added_at"`
}

type fakeApplyEntry struct {
	PlayerID int64 `json:"player_id"`
	ApplyAt  int64 `json:"apply_at"`
}

// fakeFriendHandler 只实现本文件用到的 friend 路由,路径与响应体照生产 handler。
type fakeFriendHandler struct {
	data      *fakeFriendData // /list 响应体;为 nil 时返回空好友数据
	failIDs   map[int64]bool  // /reject_request 中返回失败的目标
	removeErr string          // /remove_friend 的业务错误文案
}

type fakeFriendListReq struct {
	g.Meta   `path:"/list" method:"POST"`
	PlayerID int64 `p:"player_id"`
}

func (h *fakeFriendHandler) List(_ context.Context, req *fakeFriendListReq) (any, error) {
	if h.data == nil || h.data.PlayerID != req.PlayerID {
		return &fakeFriendData{PlayerID: req.PlayerID}, nil
	}
	return h.data, nil
}

type fakeFriendRejectReq struct {
	g.Meta `path:"/reject_request" method:"POST"`
	A      int64   `p:"a"`
	Bs     []int64 `p:"bs"`
}

func (h *fakeFriendHandler) RejectRequest(_ context.Context, req *fakeFriendRejectReq) (any, error) {
	items := make([]api.FriendBatchItem, 0, len(req.Bs))
	for _, id := range req.Bs {
		item := api.FriendBatchItem{TargetID: id, Success: !h.failIDs[id]}
		if !item.Success {
			item.Error = "申请不存在"
		}
		items = append(items, item)
	}
	return items, nil
}

type fakeFriendRemoveReq struct {
	g.Meta `path:"/remove_friend" method:"POST"`
	A      int64 `p:"a"`
	B      int64 `p:"b"`
}

func (h *fakeFriendHandler) RemoveFriend(_ context.Context, _ *fakeFriendRemoveReq) (any, error) {
	if h.removeErr != "" {
		return nil, gxyhttp.NewErrCode(1, h.removeErr)
	}
	return nil, nil
}

// newTestFriendRole 造一个注入了 miniredis 与 sqlmock 的 RoleFriend。
func newTestFriendRole(t *testing.T) *RoleFriend {
	t.Helper()
	initAllTestConfig(t)

	cli := redis.NewClient(&redis.Options{Addr: miniredis.RunT(t).Addr()})
	t.Cleanup(func() { _ = cli.Close() })
	db, _ := newGormDBForRole(t)

	role := &RoleMain{RoleID: 1001, deps: deps.Deps{DB: db, Redis: cli}}
	rf := &RoleFriend{}
	rf.SetRole(role)
	return rf
}

// TestGetRelation_ViaFriendService 覆盖 role 侧问「两人是否好友」的经服务接口那一条路径。
func TestGetRelation_ViaFriendService(t *testing.T) {
	reg := gxyservicetest.Install(t)
	reg.Serve(t, "friend", &fakeFriendHandler{data: &fakeFriendData{
		PlayerID: 1001,
		Friends:  []fakeFriendEntry{{PlayerID: 2001, AddedAt: 111}},
		Outgoing: []fakeApplyEntry{{PlayerID: 2002, ApplyAt: 222}},
	}})
	ctx := context.Background()

	// 命中好友:同时证明服务确实被调到(getRelation 会把错误吞成 stranger)。
	if got, err := getRelation(ctx, 1001, 2001); err != nil || got != relationFriend {
		t.Fatalf("friend: got (%v, %v), want (relationFriend, nil)", got, err)
	}
	if got, _ := getRelation(ctx, 1001, 2002); got != relationApplied {
		t.Errorf("applied: got %v, want relationApplied", got)
	}
	if got, _ := getRelation(ctx, 1001, 2003); got != relationStranger {
		t.Errorf("stranger: got %v, want relationStranger", got)
	}
}

// TestReqFriendList_ViaFriendService 列表解析 + role_public 缓存拼装。
func TestReqFriendList_ViaFriendService(t *testing.T) {
	reg := gxyservicetest.Install(t)
	reg.Serve(t, "friend", &fakeFriendHandler{data: &fakeFriendData{
		PlayerID: 1001,
		Friends: []fakeFriendEntry{
			{PlayerID: 2001, AddedAt: 111},
			{PlayerID: 2002, AddedAt: 222},
		},
	}})
	rf := newTestFriendRole(t)
	ctx := context.Background()

	for id, name := range map[int64]string{2001: "hero2001", 2002: "hero2002"} {
		rp := &RolePublicState{}
		rp.RoleID = id
		rp.Name = name
		setRolePublicToCache(ctx, rf.Deps().Redis, rp)
	}

	rsp, err := rf.ReqFriendList(ctx, &pb.ReqFriendList{})
	if err != nil {
		t.Fatalf("ReqFriendList: %v", err)
	}
	if rsp.Total != 2 || len(rsp.Friends) != 2 {
		t.Fatalf("total/friends = %d/%d, want 2/2", rsp.Total, len(rsp.Friends))
	}
	if got := rsp.Friends[0].PlayerInfo.Name; got != "hero2001" {
		t.Errorf("friends[0].name = %q, want hero2001", got)
	}
	if got := rsp.Friends[1].FriendSince; got != 222 {
		t.Errorf("friends[1].friend_since = %d, want 222", got)
	}
}

// TestReqFriendRejectRequest_BatchFailure 批量结果里单项失败要上抛为错误。
func TestReqFriendRejectRequest_BatchFailure(t *testing.T) {
	reg := gxyservicetest.Install(t)
	reg.Serve(t, "friend", &fakeFriendHandler{failIDs: map[int64]bool{2002: true}})
	rf := newTestFriendRole(t)

	_, err := rf.ReqFriendRejectRequest(context.Background(), &pb.ReqFriendRejectRequest{
		FromIds: []int64{2001, 2002},
	})
	if err == nil || !strings.Contains(err.Error(), "申请不存在") {
		t.Fatalf("err = %v, want 含「申请不存在」", err)
	}
}

// TestReqFriendRemove_ServiceErrorCode 生产 handler 用 HTTP 200 + code!=0 表达业务失败,
// role 侧必须转成错误,而不是当成成功。
func TestReqFriendRemove_ServiceErrorCode(t *testing.T) {
	ctx := context.Background()

	t.Run("业务失败上抛", func(t *testing.T) {
		reg := gxyservicetest.Install(t)
		reg.Serve(t, "friend", &fakeFriendHandler{removeErr: "不是好友"})
		rf := newTestFriendRole(t)

		if _, err := rf.ReqFriendRemove(ctx, &pb.ReqFriendRemove{TargetId: 2001}); err == nil ||
			!strings.Contains(err.Error(), "不是好友") {
			t.Fatalf("err = %v, want 含「不是好友」", err)
		}
	})

	t.Run("成功无错", func(t *testing.T) {
		reg := gxyservicetest.Install(t)
		reg.Serve(t, "friend", &fakeFriendHandler{})
		rf := newTestFriendRole(t)

		if _, err := rf.ReqFriendRemove(ctx, &pb.ReqFriendRemove{TargetId: 2001}); err != nil {
			t.Fatalf("err = %v, want nil", err)
		}
	})
}
