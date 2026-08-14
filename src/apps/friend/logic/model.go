package logic

import (
	"context"
	"database/sql/driver"
	"encoding/json"
	"time"

	"github.com/olekukonko/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// scanJSONBytes 兼容 driver 返回 []byte 或 string 的 JSON 反序列化。
// 注意: PG 驱动返回 []byte, 但其他驱动/中间层(mock、代理)可能返回 string,
// 类型断言过死会导致 panic(且 panic 在 database/sql 持锁期会死锁)。
func scanJSONBytes(val interface{}, dst any) error {
	if val == nil {
		return nil
	}
	var bytes []byte
	switch v := val.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return errors.Newf("unsupported scan type %T", val)
	}
	return json.Unmarshal(bytes, dst)
}

// FriendEntry 好友记录
type FriendEntry struct {
	PlayerID int64 `json:"player_id"`
	AddedAt  int64 `json:"added_at"` // unix 时间戳
}

type FriendList []FriendEntry

func (l FriendList) Value() (driver.Value, error) { return json.Marshal(l) }
func (l *FriendList) Scan(val interface{}) error {
	return scanJSONBytes(val, l)
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

func (l ApplyList) Value() (driver.Value, error) { return json.Marshal(l) }
func (l *ApplyList) Scan(val interface{}) error {
	return scanJSONBytes(val, l)
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

func (l CooldownList) Value() (driver.Value, error) { return json.Marshal(l) }
func (l *CooldownList) Scan(val interface{}) error {
	return scanJSONBytes(val, l)
}

// FriendData 单行全量玩家好友数据
type FriendData struct {
	PlayerID  int64        `gorm:"column:player_id;primaryKey"`
	Friends   FriendList   `gorm:"column:friends;type:jsonb;default:'[]'"`
	Incoming  ApplyList    `gorm:"column:incoming;type:jsonb;default:'[]'"` // 收到谁的申请（未处理）
	Outgoing  ApplyList    `gorm:"column:outgoing;type:jsonb;default:'[]'"` // 向谁发过申请（未处理）
	Cooldowns CooldownList `gorm:"column:cooldowns;type:jsonb;default:'[]'"`
	UpdateAt  time.Time    `gorm:"column:update_at;autoUpdateTime"`
}

func (FriendData) TableName() string { return "friend_data" }

// FriendRelation 好友关系表（一条好友关系两行）
type FriendRelation struct {
	PlayerID int64 `gorm:"column:player_id;primaryKey"`
	FriendID int64 `gorm:"column:friend_id;primaryKey"`
	AddedAt  int64 `gorm:"column:added_at"`
}

func (FriendRelation) TableName() string { return "friend_relation" }

// ---- DB 操作 ----

func openTx(ctx context.Context, db *gorm.DB) *gorm.DB {
	return db.WithContext(ctx).Begin()
}

func lockRow(tx *gorm.DB, playerID int64) (*FriendData, error) {
	var d FriendData
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
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

func addRelation(tx *gorm.DB, a, b, addedAt int64) error {
	return tx.Create([]*FriendRelation{
		{PlayerID: a, FriendID: b, AddedAt: addedAt},
		{PlayerID: b, FriendID: a, AddedAt: addedAt},
	}).Error
}

func removeRelation(tx *gorm.DB, a, b int64) error {
	return tx.Where(
		"(player_id = ? AND friend_id = ?) OR (player_id = ? AND friend_id = ?)", a, b, b, a,
	).Delete(&FriendRelation{}).Error
}
