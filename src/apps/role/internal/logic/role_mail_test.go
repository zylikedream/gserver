package logic

import (
	"context"
	"sync"
	"testing"
	"time"

	gamecfg "gserver/gameconfig/gosrc"
	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/logic/bag"
	"gserver/src/lib/rolelib"
	"gserver/src/pkg/gameconfig"

	proto "google.golang.org/protobuf/proto"
	"gorm.io/gorm/schema"
)

func initMailTestConfig(t *testing.T, rows ...map[string]any) {
	t.Helper()
	initAllTestConfig(t)
	if len(rows) > 0 {
		tbMailConfig, err := gamecfg.NewGardenTbMailConfig(rows)
		if err != nil {
			t.Fatal(err)
		}
		gameconfig.Get().TbMailConfig = tbMailConfig
	}
}

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
		mailCache: []MailView{
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
		mailCache: []MailView{
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
		mailCache: []MailView{
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
		mailCache: []MailView{
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
		mailCache: []MailView{
			{ID: 1},
			{ID: 2},
		},
	}
	m := mail.findMail(999)
	if m != nil {
		t.Errorf("expected nil for not found, got %v", m)
	}
}

func TestBuildMailViews_MergesContentWithPlayerState(t *testing.T) {
	initMailTestConfig(t)
	now := time.Now().Unix()
	personal := []PersonalMailItem{
		{ID: 11, RoleID: 1001, Title: "personal", Content: "pc", SendAt: now, ExpireAt: now + 100},
	}
	system := []SysMailItem{
		{ID: 12, Title: "system", Content: "sc", SendAt: now + 1, ExpireAt: now + 100},
		{ID: 13, Title: "deleted", Content: "dc", SendAt: now + 2, ExpireAt: now + 100},
	}
	states := MailStateMap{
		"11": {MailID: 11, IsRead: true},
		"12": {MailID: 12, IsSysMail: true, IsClaimed: true},
		"13": {MailID: 13, IsSysMail: true, IsDeleted: true},
	}

	views := buildMailViews(personal, system, states, now, 10)

	if len(views) != 2 {
		t.Fatalf("expected 2 visible mails, got %d", len(views))
	}
	if views[0].ID != 12 || !views[0].IsClaimed {
		t.Fatalf("expected system mail with claimed state first, got %+v", views[0])
	}
	if views[1].ID != 11 || !views[1].IsRead {
		t.Fatalf("expected personal mail with read state second, got %+v", views[1])
	}
}

func TestMailStateKey_UsesGlobalMailID(t *testing.T) {
	if got := mailStateKey(123); got != "123" {
		t.Fatalf("expected global id key 123, got %q", got)
	}
}

func TestReceiveSystemMails_AppendsStateAndAdvancesPointer(t *testing.T) {
	state := &RoleMailState{
		LastSysMailID: 10,
		States: MailStateMap{
			"7": {MailID: 7, IsSysMail: true},
		},
	}
	sysMails := []SysMailItem{
		{ID: 9},
		{ID: 11},
		{ID: 12},
	}

	receiveSystemMails(state, sysMails)

	if state.LastSysMailID != 12 {
		t.Fatalf("expected last sys mail id 12, got %d", state.LastSysMailID)
	}
	if got := state.States["11"]; got.MailID != 11 || !got.IsSysMail {
		t.Fatalf("expected sys mail 11 state, got %+v", got)
	}
	if got := state.States["12"]; got.MailID != 12 || !got.IsSysMail {
		t.Fatalf("expected sys mail 12 state, got %+v", got)
	}
	if _, ok := state.States["9"]; ok {
		t.Fatal("did not expect old sys mail 9 to be received")
	}
	if !state.IsDirty() {
		t.Fatal("expected state marked dirty after receiving new system mails")
	}
}

func TestReceivePersonalMails_AppendsStateForEachMail(t *testing.T) {
	state := &RoleMailState{
		States: MailStateMap{
			"8": {MailID: 8, IsRead: true},
		},
	}
	personal := []PersonalMailItem{
		{ID: 8},
		{ID: 9},
	}

	receivePersonalMails(state, personal)

	if got := state.States["8"]; got.MailID != 8 || !got.IsRead || got.IsSysMail {
		t.Fatalf("expected existing personal mail state preserved, got %+v", got)
	}
	if got := state.States["9"]; got.MailID != 9 || got.IsSysMail {
		t.Fatalf("expected new personal mail 9 state, got %+v", got)
	}
	if !state.IsDirty() {
		t.Fatal("expected state marked dirty after receiving new personal mail")
	}
}

func TestSystemMailIDsFromState_UsesMailIDField(t *testing.T) {
	ids := systemMailIDsFromState(MailStateMap{
		"legacy-key": {MailID: 21, IsSysMail: true},
		"22":         {MailID: 22},
		"23":         {MailID: 23, IsSysMail: true, IsDeleted: true},
	})

	if len(ids) != 1 || ids[0] != 21 {
		t.Fatalf("expected only system mail id 21, got %+v", ids)
	}
}

func TestTrimMailViews_PreservesUnclaimedAttachmentMails(t *testing.T) {
	mails := []MailView{
		{ID: 5, SendAt: 5},
		{ID: 4, SendAt: 4, Attachments: []bag.Good{{GoodID: 1, Num: 1}}},
		{ID: 3, SendAt: 3},
		{ID: 2, SendAt: 2, Attachments: []bag.Good{{GoodID: 1, Num: 1}}},
		{ID: 1, SendAt: 1},
	}

	trimmed := trimMailViews(mails, 3)

	if len(trimmed) != 3 {
		t.Fatalf("expected 3 mails, got %d", len(trimmed))
	}
	want := []int64{4, 2, 5}
	for i, id := range want {
		if trimmed[i].ID != id {
			t.Fatalf("index %d expected mail %d, got %+v", i, id, trimmed[i])
		}
	}
}

func TestMailRuntimeConfig_UsesGameConfig(t *testing.T) {
	mailConfigRows := loadTestTable(t, "garden_tbmailconfig", map[string]any{
		"mail_max_count":         float64(3),
		"default_expire_days":    float64(7),
		"one_key_claim_limit":    float64(2),
		"title_limit":            float64(8),
		"content_limit":          float64(16),
		"allow_delete_unclaimed": true,
	})
	initMailTestConfig(t, mailConfigRows[len(mailConfigRows)-1])

	cfg := mailRuntimeConfig(gameconfig.Get())

	if cfg.MailMaxCount != 3 || cfg.DefaultExpireDays != 7 || cfg.OneKeyClaimLimit != 2 || !cfg.AllowDeleteUnclaimed {
		t.Fatalf("unexpected mail config: %+v", cfg)
	}
}

func TestMailRuntimeConfig_RequiresMailConfig(t *testing.T) {
	// 本地构造缺失配表实例,不碰全局(避免污染其他测试的配表状态)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when mail config is missing")
		}
	}()

	_ = mailRuntimeConfig(&gameconfig.GameConfig{Tables: &gamecfg.Tables{}})
}

func TestValidateSendMailOpts_RejectsOverLimitText(t *testing.T) {
	cfg := &gamecfg.GardenMailConfig{TitleLimit: 3, ContentLimit: 5}

	if err := validateSendMailOpts(SendMailOpts{Title: "1234", Content: "ok"}, cfg); err == nil {
		t.Fatal("expected title limit error")
	}
	if err := validateSendMailOpts(SendMailOpts{Title: "ok", Content: "123456"}, cfg); err == nil {
		t.Fatal("expected content limit error")
	}
	if err := validateSendMailOpts(SendMailOpts{Title: "好好好", Content: "花花花花花"}, cfg); err != nil {
		t.Fatalf("expected rune-counted text within limit, got %v", err)
	}
}

func TestMailContentIDsUseGlobalSequenceWithoutBigserial(t *testing.T) {
	for _, model := range []any{&PersonalMailItem{}, &SysMailItem{}} {
		parsed, err := schema.Parse(model, &sync.Map{}, schema.NamingStrategy{})
		if err != nil {
			t.Fatal(err)
		}
		id := parsed.LookUpField("ID")
		if id == nil {
			t.Fatal("expected ID field")
		}
		if id.AutoIncrement {
			t.Fatalf("%T ID should not be gorm auto increment", model)
		}
		if id.DefaultValue != "nextval('mail_global_id_seq')" {
			t.Fatalf("%T expected mail_global_id_seq default, got %q", model, id.DefaultValue)
		}
	}
}

func TestRoleMainOnNotifyMessage_RefreshesMailBeforeNotify(t *testing.T) {
	ctx := context.Background()
	role := &RoleMain{}
	role.Mail = &RoleMail{}

	refreshCalled := false
	origRefresh := refreshMailCache
	refreshMailCache = func(mail *RoleMail, _ context.Context) error {
		refreshCalled = true
		mail.mailCache = []MailView{
			{ID: 1, IsRead: false},
			{ID: 2, IsRead: true, Attachments: []bag.Good{{GoodID: 1, Num: 1}}},
		}
		return nil
	}
	t.Cleanup(func() { refreshMailCache = origRefresh })

	var sent proto.Message
	origSend := sendClient
	sendClient = func(_ *RoleMain, _ context.Context, msg proto.Message) {
		sent = msg
	}
	t.Cleanup(func() { sendClient = origSend })

	if err := role.OnNotifyMessage(ctx, &rolelib.OnRoleNotifyMsg{Msg: &pb.NotifyMailUpdate{}}); err != nil {
		t.Fatal(err)
	}

	if !refreshCalled {
		t.Fatal("expected mail cache refresh before notify")
	}
	if _, ok := sent.(*pb.NotifyMailUpdate); !ok {
		t.Fatalf("expected NotifyMailUpdate sent, got %T", sent)
	}
}
