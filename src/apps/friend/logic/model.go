package logic

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"time"

	"gserver/core/gxypgx"
	"gorm.io/gorm"
)

// FriendEntry 好友记录
type FriendEntry struct {
	PlayerID int64 `json:"player_id"`
	AddedAt  int64 `json:"added_at"` // unix 时间戳
}

type FriendList []FriendEntry

func (l FriendList) Value() (driver.Value, error)  { return json.Marshal(l) }
func (l *FriendList) Scan(val interface{}) error {
	if val == nil {
		return nil
	}
	return json.Unmarshal(val.([]byte), l)
}
func (l FriendList) Has(id int64) bool {
	for _, v := range l {
		if v.PlayerID == id {
			return true
		}
	}
	return false
}
func (l FriendList) Remove(id int64) FriendList {
	for i, v := range l {
		if v.PlayerID == id {
			return append(l[:i], l[i+1:]...)
		}
	}
	return l
}

// ApplyEntry 申请记录
type ApplyEntry struct {
	PlayerID int64 `json:"player_id"`
	ApplyAt  int64 `json:"apply_at"` // unix 时间戳
}

type ApplyList []ApplyEntry

func (l ApplyList) Value() (driver.Value, error)  { return json.Marshal(l) }
func (l *ApplyList) Scan(val interface{}) error {
	if val == nil {
		return nil
	}
	return json.Unmarshal(val.([]byte), l)
}
func (l ApplyList) Has(id int64) bool {
	for _, v := range l {
		if v.PlayerID == id {
			return true
		}
	}
	return false
}
func (l ApplyList) Remove(id int64) ApplyList {
	for i, v := range l {
		if v.PlayerID == id {
			return append(l[:i], l[i+1:]...)
		}
	}
	return l
}

// CooldownEntry 冷却记录
type CooldownEntry struct {
	TargetID int64 `json:"target_id"`
	Until    int64 `json:"until"` // unix 时间戳
}

type CooldownList []CooldownEntry

func (l CooldownList) Value() (driver.Value, error)  { return json.Marshal(l) }
func (l *CooldownList) Scan(val interface{}) error {
	if val == nil { return nil }
	return json.Unmarshal(val.([]byte), l)
}

// FriendData 单行全量玩家好友数据
type FriendData struct {
	PlayerID  int64         `gorm:"column:player_id;primaryKey"`
	Friends   FriendList    `gorm:"column:friends;type:jsonb;default:'[]'"`
	Incoming  ApplyList     `gorm:"column:incoming;type:jsonb;default:'[]'"`  // 收到谁的申请（未处理）
	Outgoing  ApplyList     `gorm:"column:outgoing;type:jsonb;default:'[]'"`  // 向谁发过申请（未处理）
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
