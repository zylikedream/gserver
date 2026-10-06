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

	"github.com/DATA-DOG/go-sqlmock"
	proto "google.golang.org/protobuf/proto"
	"gorm.io/gorm/schema"
)

// initMailTestConfig 初始化游戏配表,可选地覆盖 mail_config 行。
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

func TestBuildMailViews_MergesContentWithPlayerState(t *testing.T) {
	initAllTestConfig(t)
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
	role, subj, mock := spawnTestRole(t, 1001)
	mail := &RoleMail{RoleModule: RoleModule{RoleID: role.RoleID, Role: role}}
	role.Mail = mail
	if err := mail.OnModInit(ctx); err != nil {
		t.Fatal(err)
	}

	// 刷新是真跑的:拉个人邮件(无系统邮件时不再多查一轮)。
	mock.ExpectQuery(`SELECT \* FROM "personal_mail" WHERE role_id = \$1 ORDER BY id DESC LIMIT \$2`).
		WithArgs(int64(1001), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "role_id", "title", "content", "attachments", "send_at", "expire_at"}).
			AddRow(1, 1001, "m1", "body", nil, time.Now().Unix(), 0))
	mock.ExpectQuery(`SELECT \* FROM "sys_mail" WHERE id > \$1 AND \(expire_at = 0 OR expire_at >= \$2\) ORDER BY id ASC LIMIT \$3`).
		WithArgs(int64(0), sqlmock.AnyArg(), sqlmock.AnyArg()).
		WillReturnRows(sqlmock.NewRows([]string{"id", "title", "content", "attachments", "create_at", "expire_at"}))

	notify := &pb.NotifyMailUpdate{MailId: 1}
	if err := role.OnNotifyMessage(ctx, &rolelib.OnRoleNotifyMsg{Msg: notify}); err != nil {
		t.Fatal(err)
	}
	if len(mail.mailCache) == 0 {
		t.Fatal("expected mail cache refreshed")
	}
	subj.ShouldSend().Where(clientMsgMatcher(func(m proto.Message) bool {
		inv, ok := m.(*pb.NotifyMailUpdate)
		return ok && inv.MailId == 1
	})).Assert()
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("sql expectations not met: %v", err)
	}
}

// TestMailRuntimeConfig_UsesGameConfig:mailRuntimeConfig 必须把配表 mail_config 行
// 原样读出来(MailMaxCount / DefaultExpireDays / OneKeyClaimLimit / AllowDeleteUnclaimed)。
// 为什么需要:该函数被 role_mail_api 的列表/领取/删除三条路径调用,配表读取错一列会
// 表现为「邮件上限不对」或「未领取附件允许删除」——都是运营配置事故而非代码崩溃。
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

// TestMailRuntimeConfig_RequiresMailConfig:配表缺 mail_config 行时必须 panic,而不是
// 返回一个全零配置被上层当成「上限 0 / 不过期」继续跑。
// 为什么需要:全零配置会让每封邮件都立刻过期或领取上限为 0,表现为玩家看到空邮箱而
// 服务端无任何报错。panic 是刻意的快速失败。
func TestMailRuntimeConfig_RequiresMailConfig(t *testing.T) {
	// 本地构造缺失配表实例,不碰全局(避免污染其他测试的配表状态)
	defer func() {
		if recover() == nil {
			t.Fatal("expected panic when mail config is missing")
		}
	}()

	_ = mailRuntimeConfig(&gameconfig.GameConfig{Tables: &gamecfg.Tables{}})
}
