package logic

import (
	"testing"
	"time"

	"gserver/src/apps/role/internal/logic/bag"
)

// ========== calcRedDot ==========

func TestCalcRedDot_Empty(t *testing.T) {
	mail := &RoleMail{mailCache: nil}
	unread, unclaimed := mail.calcRedDot()
	if unread != 0 || unclaimed != 0 {
		t.Errorf("empty cache expected 0,0 got %d,%d", unread, unclaimed)
	}
}

func TestCalcRedDot_Basic(t *testing.T) {
	mail := &RoleMail{
		mailCache: []MailEntry{
			{ID: 1, IsRead: false},
			{ID: 2, IsRead: true},
			{ID: 3, IsRead: false, Attachments: []bag.Good{{GoodID: 101, Num: 5}}},
			{ID: 4, IsRead: true, Attachments: []bag.Good{{GoodID: 102, Num: 1}}, IsClaimed: true},
		},
	}
	unread, unclaimed := mail.calcRedDot()
	if unread != 2 {
		t.Errorf("expected 2 unread, got %d", unread)
	}
	if unclaimed != 1 {
		t.Errorf("expected 1 unclaimed, got %d", unclaimed)
	}
}

func TestCalcRedDot_Expired(t *testing.T) {
	now := time.Now().Unix()
	mail := &RoleMail{
		mailCache: []MailEntry{
			{ID: 1, IsRead: false, ExpireAt: now - 100},
			{ID: 2, IsRead: false, ExpireAt: now + 1000},
			{ID: 3, IsRead: false, Attachments: []bag.Good{{GoodID: 101, Num: 5}}, ExpireAt: now - 100},
		},
	}
	unread, unclaimed := mail.calcRedDot()
	if unread != 1 {
		t.Errorf("expected 1 unread (expired excluded), got %d", unread)
	}
	if unclaimed != 0 {
		t.Errorf("expected 0 unclaimed (expired excluded), got %d", unclaimed)
	}
}

func TestCalcRedDot_ExpireAtZero(t *testing.T) {
	mail := &RoleMail{
		mailCache: []MailEntry{
			{ID: 1, IsRead: false, ExpireAt: 0},
		},
	}
	unread, unclaimed := mail.calcRedDot()
	if unread != 1 {
		t.Errorf("expected 1 unread (expire_at=0 means never expire), got %d", unread)
	}
	if unclaimed != 0 {
		t.Errorf("expected 0 unclaimed, got %d", unclaimed)
	}
}

// ========== findMail ==========

func TestFindMail(t *testing.T) {
	mail := &RoleMail{
		mailCache: []MailEntry{
			{ID: 1, Title: "first"},
			{ID: 2, Title: "second"},
			{ID: 3, Title: "third"},
		},
	}
	m := mail.findMail(2)
	if m == nil || m.Title != "second" {
		t.Errorf("expected mail 2, got %v", m)
	}
}

func TestFindMail_NotFound(t *testing.T) {
	mail := &RoleMail{
		mailCache: []MailEntry{
			{ID: 1},
			{ID: 2},
		},
	}
	m := mail.findMail(999)
	if m != nil {
		t.Errorf("expected nil for not found, got %v", m)
	}
}
