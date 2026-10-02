package util

import (
	"testing"
)

// TestListDelete_Middle:契约——删除第一个匹配项 3,并把其后元素整体前移一位,
// 长度减 1,结果为 [1,2,4,5](就地压缩,与调用方共享同一底层数组)。
// 为什么需要:前移错一位(少复制一个元素)会残留重复值,列表按序遍历时静默多出一项。
func TestListDelete_Middle(t *testing.T) {
	list := []int{1, 2, 3, 4, 5}
	list = ListDelete(list, 3)
	if len(list) != 4 {
		t.Fatalf("expected len 4, got %d", len(list))
	}
	if list[2] != 4 {
		t.Fatalf("expected [1,2,4,5], got %v", list)
	}
}

// TestListDelete_First:契约——匹配项在 i==0 时同样只删一项,得到 [2,3],不得 panic。
// 为什么需要:i==0 是最容易 off-by-one 的位置(起点写成 i+1 就变成删掉 2),结果是 [1,2],
// 调用方会把已删除的元素当成仍在列表里继续处理。
func TestListDelete_First(t *testing.T) {
	list := []int{1, 2, 3}
	list = ListDelete(list, 1)
	if len(list) != 2 || list[0] != 2 {
		t.Fatalf("expected [2,3], got %v", list)
	}
}

// TestListDelete_Last:契约——匹配项在末尾(i==len-1)时长度为 2、末位消失,不得越界 panic。
// 为什么需要:末尾分支追加的是空切片,偏移写错会 slice bounds out of range,
// 整轮清理流程直接挂死,后续元素再也不会被摘除。
func TestListDelete_Last(t *testing.T) {
	list := []int{1, 2, 3}
	list = ListDelete(list, 3)
	if len(list) != 2 || list[1] != 2 {
		t.Fatalf("expected [1,2], got %v", list)
	}
}

// TestListDelete_NotFound:契约——没有匹配项时原样返回,长度与内容都不变。
// 为什么需要:按过期 ID 摘除成员的清理逻辑依赖"没找到就什么都不做";
// 若误删任意一个元素,会静默踢掉无关的在线玩家,且没有任何报错。
func TestListDelete_NotFound(t *testing.T) {
	list := []int{1, 2, 3}
	list = ListDelete(list, 99)
	if len(list) != 3 {
		t.Fatalf("expected len 3, got %d", len(list))
	}
}

// TestListDelete_Duplicate:契约——文档承诺"删除第一个匹配项",{1,2,2,3} 删 2 应得 [1,2,3]
// (靠前的那个 2 消失)。当前断言只查 len==3 && list[1]==2,而删首或删尾都得到 [1,2,3],
// 因此这个断言无法区分两者。
// 为什么需要:若实现改成删最后一个匹配项,现有断言仍然全绿,"取首个"的语义被静默改变。
func TestListDelete_Duplicate(t *testing.T) {
	list := []int{1, 2, 2, 3}
	list = ListDelete(list, 2)
	if len(list) != 3 {
		t.Fatalf("expected len 3, got %d", len(list))
	}
	if list[1] != 2 {
		t.Fatalf("expected second 2 remaining, got %v", list)
	}
}

// TestListDelete_Empty:契约——空切片调用返回空切片,不 panic。
// 为什么需要:定时清理在列表为空时同样走这条分支,一次 panic 就让整轮摘除中断,
// 过期成员从此永久滞留。
func TestListDelete_Empty(t *testing.T) {
	list := []int{}
	list = ListDelete(list, 1)
	if len(list) != 0 {
		t.Fatalf("expected empty, got %v", list)
	}
}

// TestListDeleteFunc_CustomCondition:契约——谓词由调用方给出,只删第一个使谓词为真的元素,
// 其余元素顺序保持不变。
// 为什么需要:按 struct 字段(如职位)清理的调用方依赖"只删一个";若误删全部匹配项,
// 整批数据被清空且无报错。
func TestListDeleteFunc_CustomCondition(t *testing.T) {
	type item struct {
		ID   int
		Name string
	}
	list := []item{{1, "a"}, {2, "b"}, {3, "c"}}
	list = ListDeleteFunc(list, func(i item) bool { return i.Name == "b" })
	if len(list) != 2 || list[0].Name != "a" || list[1].Name != "c" {
		t.Fatalf("unexpected result: %v", list)
	}
}

// TestListMember_Found:契约——元素存在时返回 true。
// 为什么需要:guild_actor.go 用它校验职位合法性,漏判为 false 会把非法职位直接落库。
func TestListMember_Found(t *testing.T) {
	if !ListMember([]string{"a", "b", "c"}, "b") {
		t.Fatal("expected true")
	}
}

// TestListMember_NotFound:契约——元素不在集合内时返回 false,由调用方回 ErrInvalidPosition。
// 为什么需要:这是职位白名单的唯一闸门,误判为 true 等于放开越权——客户端可把自己改成任意职位。
func TestListMember_NotFound(t *testing.T) {
	if ListMember([]string{"a", "b", "c"}, "d") {
		t.Fatal("expected false")
	}
}

// TestListMember_Empty:契约——空集合恒为 false,不 panic。
// 为什么需要:配置表为空(未加载/热更失败)时不能被判成"职位合法",否则脏配置直接放行。
func TestListMember_Empty(t *testing.T) {
	if ListMember([]int{}, 1) {
		t.Fatal("expected false for empty slice")
	}
}

// TestListMemberFunc_Match:契约——谓词命中任一元素即返回 true。
// 为什么需要:换用自定义谓词(校验带状态的成员)后,返回值决定请求放行还是拒绝,
// 判错不会报错,只会让合法操作被拒或非法操作通过。
func TestListMemberFunc_Match(t *testing.T) {
	if !ListMemberFunc([]int{1, 2, 3}, func(i int) bool { return i > 2 }) {
		t.Fatal("expected true")
	}
}

// TestListMemberFunc_NoMatch:契约——没有任何元素满足谓词时恒为 false。
// 为什么需要:谓词取错字段(永远不成立)会让所有请求被拒,功能整体不可用;
// 谓词恒真则是相反的越权,两者都只能靠这条区分。
func TestListMemberFunc_NoMatch(t *testing.T) {
	if ListMemberFunc([]int{1, 2, 3}, func(i int) bool { return i > 5 }) {
		t.Fatal("expected false")
	}
}

// TestListFind_Found:契约——命中时返回元素值与其在原切片中的下标(20, 1)。
// 为什么需要:调用方常拿下标回写或删除该元素;下标错一位就会改错对象。
func TestListFind_Found(t *testing.T) {
	val, idx := ListFind([]int{10, 20, 30}, 20)
	if idx != 1 || val != 20 {
		t.Fatalf("expected (20, 1), got (%d, %d)", val, idx)
	}
}

// TestListFind_NotFound:契约——未命中时返回 (-1, 零值),不是 (0, 第一个元素)。
// 为什么需要:调用方惯例以 idx>=0 判存在;未命中却返回 0 下标,会把"没找到"
// 当成"第一个元素",静默改错数据。
func TestListFind_NotFound(t *testing.T) {
	val, idx := ListFind([]int{10, 20, 30}, 99)
	if idx != -1 {
		t.Fatalf("expected -1, got %d", idx)
	}
	if val != 0 {
		t.Fatalf("expected zero value, got %d", val)
	}
}

// TestListFindFunc_Custom:契约——谓词版返回第一个满足条件的元素及其原下标。
// 为什么需要:按 Name 查用户时若下标错位,后续的更新/删除会写到别人身上。
func TestListFindFunc_Custom(t *testing.T) {
	type user struct {
		ID   int
		Name string
	}
	users := []user{{1, "alice"}, {2, "bob"}, {3, "charlie"}}
	u, idx := ListFindFunc(users, func(u user) bool { return u.Name == "bob" })
	if idx != 1 || u.Name != "bob" {
		t.Fatalf("expected bob at 1, got %v at %d", u, idx)
	}
}

// TestListFindFunc_NotFound:契约——谓词无一满足时返回零值结构体(ID 全为 0)与 -1。
// 为什么需要:零值结构体一旦被当成真实对象写库或下发,就产生 ID=0 的脏数据。
func TestListFindFunc_NotFound(t *testing.T) {
	type user struct {
		ID   int
		Name string
	}
	users := []user{{1, "alice"}}
	u, idx := ListFindFunc(users, func(u user) bool { return u.Name == "bob" })
	if idx != -1 {
		t.Fatalf("expected -1, got %d", idx)
	}
	if u.ID != 0 {
		t.Fatalf("expected zero value, got %+v", u)
	}
}
