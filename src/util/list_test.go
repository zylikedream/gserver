package util

import (
	"testing"
)

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

func TestListDelete_First(t *testing.T) {
	list := []int{1, 2, 3}
	list = ListDelete(list, 1)
	if len(list) != 2 || list[0] != 2 {
		t.Fatalf("expected [2,3], got %v", list)
	}
}

func TestListDelete_Last(t *testing.T) {
	list := []int{1, 2, 3}
	list = ListDelete(list, 3)
	if len(list) != 2 || list[1] != 2 {
		t.Fatalf("expected [1,2], got %v", list)
	}
}

func TestListDelete_NotFound(t *testing.T) {
	list := []int{1, 2, 3}
	list = ListDelete(list, 99)
	if len(list) != 3 {
		t.Fatalf("expected len 3, got %d", len(list))
	}
}

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

func TestListDelete_Empty(t *testing.T) {
	list := []int{}
	list = ListDelete(list, 1)
	if len(list) != 0 {
		t.Fatalf("expected empty, got %v", list)
	}
}

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

func TestListMember_Found(t *testing.T) {
	if !ListMember([]string{"a", "b", "c"}, "b") {
		t.Fatal("expected true")
	}
}

func TestListMember_NotFound(t *testing.T) {
	if ListMember([]string{"a", "b", "c"}, "d") {
		t.Fatal("expected false")
	}
}

func TestListMember_Empty(t *testing.T) {
	if ListMember([]int{}, 1) {
		t.Fatal("expected false for empty slice")
	}
}

func TestListMemberFunc_Match(t *testing.T) {
	if !ListMemberFunc([]int{1, 2, 3}, func(i int) bool { return i > 2 }) {
		t.Fatal("expected true")
	}
}

func TestListMemberFunc_NoMatch(t *testing.T) {
	if ListMemberFunc([]int{1, 2, 3}, func(i int) bool { return i > 5 }) {
		t.Fatal("expected false")
	}
}

func TestListFind_Found(t *testing.T) {
	val, idx := ListFind([]int{10, 20, 30}, 20)
	if idx != 1 || val != 20 {
		t.Fatalf("expected (20, 1), got (%d, %d)", val, idx)
	}
}

func TestListFind_NotFound(t *testing.T) {
	val, idx := ListFind([]int{10, 20, 30}, 99)
	if idx != -1 {
		t.Fatalf("expected -1, got %d", idx)
	}
	if val != 0 {
		t.Fatalf("expected zero value, got %d", val)
	}
}

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
