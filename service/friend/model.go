package friend

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"time"

	"gserver/core/gxypgx"
	"gorm.io/gorm"
)

// Int64List JSONB 支持的 int64 切片
type Int64List []int64

func (l Int64List) Value() (driver.Value, error)  { return json.Marshal(l) }
func (l *Int64List) Scan(val interface{}) error {
	if val == nil { return nil }
	return json.Unmarshal(val.([]byte), l)
}
func (l Int64List) Has(id int64) bool {
	for _, v := range l { if v == id { return true } }
	return false
}

// CooldownEntry 冷却记录
type CooldownEntry struct {
	TargetID int64 `json:"target_id"`
	Until    int64 `json:"until"` // unix 时间戳
}

// CooldownList JSONB 支持的冷却列表
type CooldownList []CooldownEntry

func (l CooldownList) Value() (driver.Value, error)  { return json.Marshal(l) }
func (l *CooldownList) Scan(val interface{}) error {
	if val == nil { return nil }
	return json.Unmarshal(val.([]byte), l)
}

// FriendData 单行全量玩家好友数据
type FriendData struct {
	PlayerID  int64         `gorm:"column:player_id;primaryKey"`
	Friends   Int64List     `gorm:"column:friends;type:jsonb;default:'[]'"`
	Incoming  Int64List     `gorm:"column:incoming;type:jsonb;default:'[]'"`  // 收到谁的申请（未处理）
	Outgoing  Int64List     `gorm:"column:outgoing;type:jsonb;default:'[]'"`  // 向谁发过申请（未处理）
	Cooldowns CooldownList  `gorm:"column:cooldowns;type:jsonb;default:'[]'"`
	UpdateAt  time.Time     `gorm:"column:update_at;autoUpdateTime"`
}

func (FriendData) TableName() string { return "friend_data" }

// ---- DB 操作 ----

func openTx(ctx context.Context) *gorm.DB {
	return gxypgx.DB().WithContext(ctx).Begin()
}

func lockRow(tx *gorm.DB, playerID int64) (*FriendData, error) {
	var d FriendData
	err := tx.Set("gorm:query_option", "FOR UPDATE").
		First(&d, playerID).Error
	if err == nil {
		return &d, nil
	}
	if err == gorm.ErrRecordNotFound {
		d = FriendData{PlayerID: playerID}
		return &d, tx.Create(&d).Error
	}
	return nil, err
}

func saveRow(tx *gorm.DB, d *FriendData) error {
	return tx.Save(d).Error
}
