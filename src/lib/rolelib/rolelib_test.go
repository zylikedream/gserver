package rolelib

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"gserver/core/gxyactor"
	"gserver/protocol/pb"

	"github.com/asynkron/protoactor-go/actor"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/anypb"
)

// notifyEnv 持有所有可替换函数变量的 fake 状态。
type notifyEnv struct {
	selfNode     string
	locateValue  string
	locateErr    error
	locateCalls  []int64
	localPid     gxyactor.PID
	localLookups []string
	sendErr      error
	sent         []*OnRoleNotifyMsg
	pubErr       error
	published    []string
	allPids      []gxyactor.PID
}

// injectFakes 注入 fake 并在测试结束时恢复原实现。
func injectFakes(t *testing.T, env *notifyEnv) {
	t.Helper()
	origActor := actorNodeInstance
	origLocate := roleLocateNode
	origGetLocal := getLocalActor
	origGetAll := getLocalActorAll
	origSend := localSend
	origPub := mqPublish
	origSub := mqSubscribe

	actorNodeInstance = func() string { return env.selfNode }
	roleLocateNode = func(ctx context.Context, roleID int64) (string, error) {
		env.locateCalls = append(env.locateCalls, roleID)
		return env.locateValue, env.locateErr
	}
	getLocalActor = func(kind, id string) gxyactor.PID {
		env.localLookups = append(env.localLookups, kind+":"+id)
		return env.localPid
	}
	getLocalActorAll = func(kind string) []gxyactor.PID { return env.allPids }
	localSend = func(ctx context.Context, pid gxyactor.PID, message any) error {
		env.sent = append(env.sent, message.(*OnRoleNotifyMsg))
		return env.sendErr
	}
	mqPublish = func(ctx context.Context, topic, msg string) error {
		env.published = append(env.published, topic+"|"+msg)
		return env.pubErr
	}
	mqSubscribe = func(ctx context.Context, topic string, handler func(ctx context.Context, msg string) error) error {
		return nil
	}
	t.Cleanup(func() {
		actorNodeInstance = origActor
		roleLocateNode = origLocate
		getLocalActor = origGetLocal
		getLocalActorAll = origGetAll
		localSend = origSend
		mqPublish = origPub
		mqSubscribe = origSub
	})
}

func buildPayload(t *testing.T, target int64, msg proto.Message) string {
	t.Helper()
	anyMsg := &anypb.Any{}
	if err := anypb.MarshalFrom(anyMsg, msg, proto.MarshalOptions{}); err != nil {
		t.Fatalf("marshal any: %v", err)
	}
	b, err := json.Marshal(&roleNotifyMsg{TargetRoleID: target, Msg: anyMsg, CreatedAt: 1})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}
	return string(b)
}

func TestPublishRoleNotify_InvalidTargetSkipsLookup(t *testing.T) {
	env := &notifyEnv{}
	injectFakes(t, env)
	if err := PublishRoleNotify(context.Background(), 0, &pb.Ack{}); err != nil {
		t.Fatalf("PublishRoleNotify = %v, want nil", err)
	}
	if len(env.locateCalls) != 0 {
		t.Errorf("roleLocateNode called %d times for invalid target, want 0", len(env.locateCalls))
	}
}

func TestPublishRoleNotify_NilMsg(t *testing.T) {
	env := &notifyEnv{}
	injectFakes(t, env)
	if err := PublishRoleNotify(context.Background(), 10001, nil); err != nil {
		t.Fatalf("PublishRoleNotify = %v, want nil", err)
	}
	if len(env.locateCalls) != 0 {
		t.Errorf("roleLocateNode called for nil msg, want 0")
	}
}

func TestPublishRoleNotify_Offline(t *testing.T) {
	env := &notifyEnv{}
	injectFakes(t, env)
	if err := PublishRoleNotify(context.Background(), 10001, &pb.Ack{}); err != nil {
		t.Fatalf("PublishRoleNotify = %v", err)
	}
	if len(env.sent) != 0 || len(env.published) != 0 {
		t.Errorf("offline target must not send; sent=%d published=%d", len(env.sent), len(env.published))
	}
}

func TestPublishRoleNotify_LocalHit(t *testing.T) {
	pid := actor.NewPID("127.0.0.1:25011", "role_10001")
	env := &notifyEnv{selfNode: "node-1", locateValue: "node-1", localPid: pid}
	injectFakes(t, env)

	if err := PublishRoleNotify(context.Background(), 10001, &pb.Ack{}); err != nil {
		t.Fatalf("PublishRoleNotify = %v, want nil", err)
	}
	if len(env.sent) != 1 {
		t.Fatalf("localSend called %d times, want 1", len(env.sent))
	}
	if _, ok := env.sent[0].Msg.(*pb.Ack); !ok {
		t.Errorf("delivered msg type = %T, want *pb.Ack", env.sent[0].Msg)
	}
}

func TestPublishRoleNotify_LocalActorMissing(t *testing.T) {
	env := &notifyEnv{selfNode: "node-1", locateValue: "node-1", localPid: nil}
	injectFakes(t, env)
	if err := PublishRoleNotify(context.Background(), 10001, &pb.Ack{}); err != nil {
		t.Fatalf("PublishRoleNotify = %v, want nil", err)
	}
	if len(env.sent) != 0 {
		t.Errorf("localSend called for missing local actor")
	}
}

func TestPublishRoleNotify_LocalSendError(t *testing.T) {
	pid := actor.NewPID("127.0.0.1:25011", "role_10001")
	env := &notifyEnv{selfNode: "node-1", locateValue: "node-1", localPid: pid, sendErr: errors.New("send boom")}
	injectFakes(t, env)
	if err := PublishRoleNotify(context.Background(), 10001, &pb.Ack{}); err == nil {
		t.Fatal("PublishRoleNotify = nil, want local send error")
	}
}

func TestPublishRoleNotify_RemotePublish(t *testing.T) {
	env := &notifyEnv{selfNode: "node-1", locateValue: "node-2"}
	injectFakes(t, env)
	if err := PublishRoleNotify(context.Background(), 10001, &pb.Ack{}); err != nil {
		t.Fatalf("PublishRoleNotify = %v, want nil", err)
	}
	if len(env.published) != 1 {
		t.Fatalf("mqPublish called %d times, want 1", len(env.published))
	}
	topic := strings.SplitN(env.published[0], "|", 2)[0]
	if topic != "gserver:notify:role:node-2" {
		t.Errorf("publish topic = %q, want node-2 topic", topic)
	}
	if !strings.Contains(env.published[0], `"target_role_id":10001`) {
		t.Errorf("payload missing target_role_id: %s", env.published[0])
	}
}

func TestPublishRoleNotify_RemotePublishError(t *testing.T) {
	env := &notifyEnv{selfNode: "node-1", locateValue: "node-2", pubErr: errors.New("pub boom")}
	injectFakes(t, env)
	if err := PublishRoleNotify(context.Background(), 10001, &pb.Ack{}); err == nil {
		t.Fatal("PublishRoleNotify = nil, want publish error")
	}
}

func TestPublishRoleNotify_LocateErrorIsOffline(t *testing.T) {
	// locate 出错时 NodeID 为空,按 offline 吞掉(best-effort 通知,
	// 与旧 Redis GET 错误路径行为一致),不算发布失败。
	env := &notifyEnv{selfNode: "node-1", locateErr: errors.New("redis degraded")}
	injectFakes(t, env)
	if err := PublishRoleNotify(context.Background(), 10001, &pb.Ack{}); err != nil {
		t.Fatalf("PublishRoleNotify = %v, want nil for locate error", err)
	}
	if len(env.sent) != 0 || len(env.published) != 0 {
		t.Errorf("degraded locate must not send; sent=%d published=%d", len(env.sent), len(env.published))
	}
}

func TestHandleNotify_UnmarshalError(t *testing.T) {
	env := &notifyEnv{}
	injectFakes(t, env)
	r := &RoleNotify{}
	if err := r.handleNotify(context.Background(), "not-json"); err == nil {
		t.Fatal("handleNotify = nil, want unmarshal error")
	}
}

func TestHandleNotify_InvalidTarget(t *testing.T) {
	env := &notifyEnv{}
	injectFakes(t, env)
	r := &RoleNotify{}
	if err := r.handleNotify(context.Background(), `{"target_role_id":0}`); err == nil {
		t.Fatal("handleNotify = nil, want invalid target error")
	}
}

func TestHandleNotify_MissingMsg(t *testing.T) {
	env := &notifyEnv{}
	injectFakes(t, env)
	r := &RoleNotify{}
	if err := r.handleNotify(context.Background(), `{"target_role_id":10001}`); err == nil {
		t.Fatal("handleNotify = nil, want missing msg error")
	}
}

func TestHandleNotify_Ok(t *testing.T) {
	pid := actor.NewPID("127.0.0.1:25011", "role_10001")
	env := &notifyEnv{localPid: pid}
	injectFakes(t, env)
	r := &RoleNotify{}

	raw := buildPayload(t, 10001, &pb.Ack{})
	if err := r.handleNotify(context.Background(), raw); err != nil {
		t.Fatalf("handleNotify = %v, want nil", err)
	}
	if len(env.sent) != 1 {
		t.Fatalf("localSend called %d times, want 1", len(env.sent))
	}
	if got := protoMessageName(env.sent[0].Msg); got != "Ack" {
		t.Errorf("consumed msg name = %q, want Ack", got)
	}
}

func TestNotifyLocalAll(t *testing.T) {
	env := &notifyEnv{
		allPids: []gxyactor.PID{
			actor.NewPID("127.0.0.1:25011", "role_1"),
			actor.NewPID("127.0.0.1:25011", "role_2"),
		},
	}
	injectFakes(t, env)
	if err := NotifyLocalAll(context.Background(), &pb.Ack{}); err != nil {
		t.Fatalf("NotifyLocalAll = %v, want nil", err)
	}
	if len(env.sent) != 2 {
		t.Errorf("localSend called %d times, want 2", len(env.sent))
	}
}

func TestGetRolePidUsesLocalActor(t *testing.T) {
	pid := actor.NewPID("127.0.0.1:25011", "role_42")
	env := &notifyEnv{localPid: pid}
	injectFakes(t, env)

	if got := GetRolePid(42); got != pid {
		t.Errorf("GetRolePid(42) = %v, want %v", got, pid)
	}
	if len(env.localLookups) != 1 || env.localLookups[0] != "role:42" {
		t.Errorf("local lookup = %v, want [role:42]", env.localLookups)
	}
}

func TestPureHelpers(t *testing.T) {
	if got := roleNotifyTopic("node-1"); got != "gserver:notify:role:node-1" {
		t.Errorf("roleNotifyTopic = %q", got)
	}
	if got := protoMessageName(&pb.Ack{}); got != "Ack" {
		t.Errorf("protoMessageName(Ack) = %q", got)
	}
	if got := protoMessageName(nil); got != "unknown" {
		t.Errorf("protoMessageName(nil) = %q, want unknown", got)
	}
}
