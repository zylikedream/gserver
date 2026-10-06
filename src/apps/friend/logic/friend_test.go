package logic

import (
	"encoding/json"
	"testing"
)

// ========== FriendList ==========

// TestFriendList_Has_True:Has 必须按 PlayerID 线性匹配,命中中间元素也返回 true。
// 为什么需要:Has 是加好友/同意申请的前置判定,误判为 true 会让已存在的申请被当成
// 好友而卡死,玩家再也加不了这个人(见 friend.go 的 SendRequest/AcceptRequest)。
func TestFriendList_Has_True(t *testing.T) {
	l := FriendList{{PlayerID: 1}, {PlayerID: 2}, {PlayerID: 3}}
	if !l.Has(2) {
		t.Fatal("expected true")
	}
}

// TestFriendList_Has_False:列表里没有该 id 时必须返回 false,而不是"找到零值"。
// 为什么需要:误判为 false 会在对方已是好友时再次写入申请行,产生重复好友数据。
func TestFriendList_Has_False(t *testing.T) {
	l := FriendList{{PlayerID: 1}, {PlayerID: 3}}
	if l.Has(2) {
		t.Fatal("expected false")
	}
}

// TestFriendList_Has_Empty:零值/空 FriendList 上 Has 必须安全返回 false。
// 为什么需要:新玩家首次 lockRow 拿到的是空列,Has 若在此越界或返回 true,
// 新号会被误判成已有好友而无法发起任何申请。
func TestFriendList_Has_Empty(t *testing.T) {
	l := FriendList{}
	if l.Has(1) {
		t.Fatal("expected false for empty")
	}
}

// TestFriendList_Remove_Middle:Remove 命中中间下标时只摘掉该条,其余元素顺序不变
// (内部走 append(l[:i], l[i+1:]...))。
// 为什么需要:AcceptRequest/RemoveFriend 都靠 Remove 清理 incoming/outgoing,
// 下标算错会静默删掉相邻的另一条申请/好友记录并写回 jsonb 列,数据不可察觉地错乱。
func TestFriendList_Remove_Middle(t *testing.T) {
	l := FriendList{{PlayerID: 1}, {PlayerID: 2}, {PlayerID: 3}}
	l = l.Remove(2)
	if len(l) != 2 {
		t.Fatalf("expected 2, got %d", len(l))
	}
	if l[0].PlayerID != 1 || l[1].PlayerID != 3 {
		t.Fatalf("unexpected: %v", l)
	}
}

// TestFriendList_Remove_NotFound:目标不存在时 Remove 原样返回,不得缩短或改写列表。
// 为什么需要:不存在即空操作;若实现返回 nil,saveRow 会把整列好友覆盖成空,
// 一次误调用就会静默清空玩家好友。
func TestFriendList_Remove_NotFound(t *testing.T) {
	l := FriendList{{PlayerID: 1}, {PlayerID: 2}}
	l = l.Remove(99)
	if len(l) != 2 {
		t.Fatalf("expected 2 (unchanged), got %d", len(l))
	}
}

// TestFriendList_Remove_First:命中首元素时结果长度 -1 且新首元素是原第二个,不能少删一个。
// 为什么需要:下标 0 是 append 重排的边界,写成 l[i+1:] 会漏删,好友被删两次逻辑,
// RemoveFriend 后残留一条幽灵好友。
func TestFriendList_Remove_First(t *testing.T) {
	l := FriendList{{PlayerID: 1}, {PlayerID: 2}, {PlayerID: 3}}
	l = l.Remove(1)
	if len(l) != 2 || l[0].PlayerID != 2 {
		t.Fatalf("unexpected: %v", l)
	}
}

// TestFriendList_Remove_Last:命中末元素时只掉尾部长度 -1,不能越界丢前一个元素。
// 为什么需要:末元素越界是 slice 拼接最常见的错误,一次同意申请就会误删另一条好友。
func TestFriendList_Remove_Last(t *testing.T) {
	l := FriendList{{PlayerID: 1}, {PlayerID: 2}, {PlayerID: 3}}
	l = l.Remove(3)
	if len(l) != 2 || l[1].PlayerID != 2 {
		t.Fatalf("unexpected: %v", l)
	}
}

// TestFriendList_Value_Scan:jsonb 列的 Value→Scan 往返必须保住 PlayerID 与 AddedAt
// 两个字段(标签 player_id/added_at)。
// 为什么需要:json tag 一旦改名,反序列化会静默丢字段,好友列表时间戳全部变 0,
// 而好友本身数量正常,线上排查不到。
func TestFriendList_Value_Scan(t *testing.T) {
	original := FriendList{{PlayerID: 100, AddedAt: 1234}, {PlayerID: 200, AddedAt: 5678}}
	val, err := original.Value()
	if err != nil {
		t.Fatal(err)
	}

	var scanned FriendList
	if err := scanned.Scan(val); err != nil {
		t.Fatal(err)
	}
	if len(scanned) != 2 {
		t.Fatalf("expected 2, got %d", len(scanned))
	}
	if scanned[0].PlayerID != 100 || scanned[1].AddedAt != 5678 {
		t.Fatalf("unexpected: %v", scanned)
	}
}

// TestFriendList_Scan_Nil:列值为 SQL NULL 时 Scan 返回 nil 且列表保持空。
// 为什么需要:老行/缺列的记录靠这条路读出"无好友";这里若报错,整个 lockRow 失败,
// 该玩家所有好友操作(申请/同意/删除)全部不可用。
func TestFriendList_Scan_Nil(t *testing.T) {
	var l FriendList
	if err := l.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if len(l) != 0 {
		t.Fatalf("expected empty, got %d", len(l))
	}
}

// TestFriendList_Value_Empty:空列表序列化必须写成合法空数组 "[]",而不是 nil slice 的 "null"。
// 为什么需要:jsonb 列写成 JSON null 后,按数组语义读取/扩展该列的代码拿到的是 null
// 而非数组,好友列表在库与内存之间语义不一致。
func TestFriendList_Value_Empty(t *testing.T) {
	l := FriendList{}
	val, err := l.Value()
	if err != nil {
		t.Fatal(err)
	}
	if string(val.([]byte)) != "[]" {
		t.Fatalf("expected [], got %s", val)
	}
}

// ========== ApplyList ==========

// TestApplyList_Has_True:Incoming/Outgoing 用同一套 Has 语义,按 PlayerID 命中返回 true。
// 为什么需要:AcceptRequest 以 me.Incoming.Has 判定申请是否存在,漏判会让过期申请被同意。
func TestApplyList_Has_True(t *testing.T) {
	l := ApplyList{{PlayerID: 10}, {PlayerID: 20}}
	if !l.Has(20) {
		t.Fatal("expected true")
	}
}

// TestApplyList_Has_False:申请列表不含该 id 时返回 false。
// 为什么需要:SendRequest 用 me.Outgoing.Has 拦截重复申请,误判会重复写申请行。
func TestApplyList_Has_False(t *testing.T) {
	l := ApplyList{{PlayerID: 10}}
	if l.Has(99) {
		t.Fatal("expected false")
	}
}

// TestApplyList_Remove:命中中间元素时只移除该条,前后元素与顺序保持不变。
// 为什么需要:同意/拒绝都靠 Remove 清理列表,下标算错会删掉别人的申请,
// 那条申请既不在申请方也不在接收方,永久丢失。
func TestApplyList_Remove(t *testing.T) {
	l := ApplyList{{PlayerID: 10}, {PlayerID: 20}, {PlayerID: 30}}
	l = l.Remove(20)
	if len(l) != 2 || l[0].PlayerID != 10 || l[1].PlayerID != 30 {
		t.Fatalf("unexpected: %v", l)
	}
}

// TestApplyList_Remove_NotFound:目标不在列表时 Remove 为空操作,列表长度不变。
// 为什么需要:误返回空列表会在 saveRow 时把整列申请覆盖掉,玩家未处理的申请被静默清空。
func TestApplyList_Remove_NotFound(t *testing.T) {
	l := ApplyList{{PlayerID: 10}}
	l = l.Remove(99)
	if len(l) != 1 {
		t.Fatalf("expected 1, got %d", len(l))
	}
}

// TestApplyList_Value_Scan:申请记录的 PlayerID/ApplyAt 往返不丢字段
// (标签 player_id/apply_at)。
// 为什么需要:apply_at 丢失后申请时间全为 0,过期申请清理与"等待对方处理"的提示都失效。
func TestApplyList_Value_Scan(t *testing.T) {
	original := ApplyList{{PlayerID: 100, ApplyAt: 9999}}
	val, err := original.Value()
	if err != nil {
		t.Fatal(err)
	}
	var scanned ApplyList
	if err := scanned.Scan(val); err != nil {
		t.Fatal(err)
	}
	if scanned[0].PlayerID != 100 || scanned[0].ApplyAt != 9999 {
		t.Fatalf("unexpected: %v", scanned)
	}
}

// TestApplyList_Scan_Nil:SQL NULL 的申请列必须扫描成空列表且不报错。
// 为什么需要:与 FriendList 同一条路径,NULL 报错会让 lockRow 失败,该玩家无法收/发任何申请。
func TestApplyList_Scan_Nil(t *testing.T) {
	var l ApplyList
	if err := l.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if len(l) != 0 {
		t.Fatalf("expected empty, got %d", len(l))
	}
}

// ========== CooldownList ==========

// TestCooldownList_Value_Scan:冷却记录的 TargetID/Until 往返不丢字段(标签 target_id/until)。
// 为什么需要:until 丢失后 RemoveFriend 写入的删除冷却期变 0,玩家可以立刻反复加删同一好友,
// 刷申请消息;冷却是防止骚扰的唯一闸门。
func TestCooldownList_Value_Scan(t *testing.T) {
	original := CooldownList{{TargetID: 1, Until: 100}, {TargetID: 2, Until: 200}}
	val, err := original.Value()
	if err != nil {
		t.Fatal(err)
	}
	var scanned CooldownList
	if err := scanned.Scan(val); err != nil {
		t.Fatal(err)
	}
	if len(scanned) != 2 || scanned[0].TargetID != 1 || scanned[1].Until != 200 {
		t.Fatalf("unexpected: %v", scanned)
	}
}

// TestCooldownList_Scan_Nil:SQL NULL 的冷却列扫描成空列表,不报错。
// 为什么需要:老玩家行没有该列内容,这里报错会让 SendRequest 的冷却检查整条锁行路径失败。
func TestCooldownList_Scan_Nil(t *testing.T) {
	var l CooldownList
	if err := l.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if len(l) != 0 {
		t.Fatalf("expected empty, got %d", len(l))
	}
}

// ========== JSON round-trip ==========

// TestFriendData_JSON_Roundtrip:整行 FriendData 的 JSON 序列化必须覆盖四个列表字段
// (friends/incoming/outgoing/cooldowns)与 player_id。
// 为什么需要:该结构也用于跨服务/缓存传输,字段漏掉会让下游读到"无好友无申请"而
// 覆盖回写,把玩家数据清空。
func TestFriendData_JSON_Roundtrip(t *testing.T) {
	d := FriendData{
		PlayerID:  42,
		Friends:   FriendList{{PlayerID: 1, AddedAt: 100}},
		Incoming:  ApplyList{{PlayerID: 2, ApplyAt: 200}},
		Outgoing:  ApplyList{{PlayerID: 3, ApplyAt: 300}},
		Cooldowns: CooldownList{{TargetID: 4, Until: 400}},
	}
	bytes, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}

	var got FriendData
	if err := json.Unmarshal(bytes, &got); err != nil {
		t.Fatal(err)
	}
	if got.PlayerID != 42 {
		t.Fatalf("expected 42, got %d", got.PlayerID)
	}
	if len(got.Friends) != 1 || got.Friends[0].PlayerID != 1 {
		t.Fatalf("unexpected friends: %v", got.Friends)
	}
	if len(got.Incoming) != 1 || got.Incoming[0].ApplyAt != 200 {
		t.Fatalf("unexpected incoming: %v", got.Incoming)
	}
	if len(got.Outgoing) != 1 || got.Outgoing[0].ApplyAt != 300 {
		t.Fatalf("unexpected outgoing: %v", got.Outgoing)
	}
	if len(got.Cooldowns) != 1 || got.Cooldowns[0].TargetID != 4 {
		t.Fatalf("unexpected cooldowns: %v", got.Cooldowns)
	}
}
