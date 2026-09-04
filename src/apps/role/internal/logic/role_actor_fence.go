package logic

import (
	"context"
	"time"

	"gserver/core/gxyactor"

	"github.com/cockroachdb/errors"
	"gorm.io/gorm"
)

var errRoleActorOwnershipLost = errors.New("role actor ownership lost")

type RoleActorFence struct {
	RoleID   int64 `gorm:"primaryKey"`
	NodeID   string
	Epoch    uint64
	UpdateAt time.Time
}

func (RoleActorFence) TableName() string {
	return "role_actor_fence"
}

func advanceRoleActorFence(ctx context.Context, db *gorm.DB, roleID int64, owner gxyactor.ActorOwner) error {
	if db == nil || roleID == 0 || owner.NodeID == "" || owner.Epoch == 0 {
		return errRoleActorOwnershipLost
	}
	result := db.WithContext(ctx).Exec(`
INSERT INTO role_actor_fence (role_id, node_id, epoch, update_at)
VALUES (?, ?, ?, ?)
ON CONFLICT (role_id) DO UPDATE
SET node_id = EXCLUDED.node_id,
    epoch = EXCLUDED.epoch,
    update_at = EXCLUDED.update_at
WHERE role_actor_fence.epoch < EXCLUDED.epoch
   OR (role_actor_fence.epoch = EXCLUDED.epoch
       AND role_actor_fence.node_id = EXCLUDED.node_id)`, roleID, owner.NodeID, owner.Epoch, time.Now())
	if result.Error != nil {
		return errors.Wrap(result.Error, "advance role actor fence")
	}
	if result.RowsAffected != 1 {
		return errRoleActorOwnershipLost
	}
	return nil
}

func lockRoleActorFence(ctx context.Context, db *gorm.DB, roleID int64, owner gxyactor.ActorOwner) error {
	if db == nil || roleID == 0 || owner.NodeID == "" || owner.Epoch == 0 {
		return errRoleActorOwnershipLost
	}
	var lockedRoleID int64
	result := db.WithContext(ctx).Raw(`
SELECT role_id
FROM role_actor_fence
WHERE role_id = ? AND node_id = ? AND epoch = ?
FOR UPDATE`, roleID, owner.NodeID, owner.Epoch).Scan(&lockedRoleID)
	if result.Error != nil {
		return errors.Wrap(result.Error, "lock role actor fence")
	}
	if result.RowsAffected != 1 || lockedRoleID != roleID {
		return errRoleActorOwnershipLost
	}
	return nil
}
