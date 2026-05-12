package logic

import (
	"encoding/json"
	"testing"
)

// ========== FriendList ==========

func TestFriendList_Has_True(t *testing.T) {
	l := FriendList{{PlayerID: 1}, {PlayerID: 2}, {PlayerID: 3}}
	if !l.Has(2) {
		t.Fatal("expected true")
	}
}

func TestFriendList_Has_False(t *testing.T) {
	l := FriendList{{PlayerID: 1}, {PlayerID: 3}}
	if l.Has(2) {
		t.Fatal("expected false")
	}
}

func TestFriendList_Has_Empty(t *testing.T) {
	l := FriendList{}
	if l.Has(1) {
		t.Fatal("expected false for empty")
	}
}

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

func TestFriendList_Remove_NotFound(t *testing.T) {
	l := FriendList{{PlayerID: 1}, {PlayerID: 2}}
	l = l.Remove(99)
	if len(l) != 2 {
		t.Fatalf("expected 2 (unchanged), got %d", len(l))
	}
}

func TestFriendList_Remove_First(t *testing.T) {
	l := FriendList{{PlayerID: 1}, {PlayerID: 2}, {PlayerID: 3}}
	l = l.Remove(1)
	if len(l) != 2 || l[0].PlayerID != 2 {
		t.Fatalf("unexpected: %v", l)
	}
}

func TestFriendList_Remove_Last(t *testing.T) {
	l := FriendList{{PlayerID: 1}, {PlayerID: 2}, {PlayerID: 3}}
	l = l.Remove(3)
	if len(l) != 2 || l[1].PlayerID != 2 {
		t.Fatalf("unexpected: %v", l)
	}
}

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

func TestFriendList_Scan_Nil(t *testing.T) {
	var l FriendList
	if err := l.Scan(nil); err != nil {
		t.Fatal(err)
	}
	if len(l) != 0 {
		t.Fatalf("expected empty, got %d", len(l))
	}
}

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

func TestApplyList_Has_True(t *testing.T) {
	l := ApplyList{{PlayerID: 10}, {PlayerID: 20}}
	if !l.Has(20) {
		t.Fatal("expected true")
	}
}

func TestApplyList_Has_False(t *testing.T) {
	l := ApplyList{{PlayerID: 10}}
	if l.Has(99) {
		t.Fatal("expected false")
	}
}

func TestApplyList_Remove(t *testing.T) {
	l := ApplyList{{PlayerID: 10}, {PlayerID: 20}, {PlayerID: 30}}
	l = l.Remove(20)
	if len(l) != 2 || l[0].PlayerID != 10 || l[1].PlayerID != 30 {
		t.Fatalf("unexpected: %v", l)
	}
}

func TestApplyList_Remove_NotFound(t *testing.T) {
	l := ApplyList{{PlayerID: 10}}
	l = l.Remove(99)
	if len(l) != 1 {
		t.Fatalf("expected 1, got %d", len(l))
	}
}

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

func TestFriendData_JSON_Roundtrip(t *testing.T) {
	d := FriendData{
		PlayerID: 42,
		Friends:  FriendList{{PlayerID: 1, AddedAt: 100}},
		Incoming: ApplyList{{PlayerID: 2, ApplyAt: 200}},
		Outgoing: ApplyList{{PlayerID: 3, ApplyAt: 300}},
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

// ========== Error variables ==========

func TestErrorVariables(t *testing.T) {
	errs := []error{ErrSelfAdd, ErrAlreadyFriend, ErrFriendFull, ErrApplyDuplicated, ErrApplyNotFound, ErrCooldown}
	for _, e := range errs {
		if e == nil || e.Error() == "" {
			t.Fatalf("error should have message: %v", e)
		}
	}
}
