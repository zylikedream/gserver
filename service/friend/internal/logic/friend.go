package logic

import (
	"context"
	"time"

	"gserver/core/gxyhttp"
	"gserver/core/gxymongo"
	"gserver/service/friend/api"
	"gserver/util"

	"github.com/gogf/gf/v2/os/glog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

// FriendController 好友控制器
type FriendController struct{}

// NewFriendController 创建好友控制器实例
func NewFriendController() *FriendController {
	return &FriendController{}
}

const (
	// 集合名称
	FriendRelationCol = "friend_relation"
)

const (
	FRIENND_STATE_APPLY  = 1
	FRIENND_STATE_FRIEND = 2
)

// Apply 申请添加好友
func (f *FriendController) Apply(ctx context.Context, req *api.ApplyFriendReq) (*api.ApplyFriendRes, error) {
	roleID := req.RoleID
	friendID := req.FriendID

	// 检查是否已经是好友
	isFriend, err := f.isFriend(ctx, roleID, friendID)
	if err != nil {
		return nil, err
	}
	if isFriend {
		return nil, ErrAlreadyFriend
	}

	// 检查是否已经发送过申请
	applyExists, err := f.applyExists(ctx, roleID, friendID)
	if err != nil {
		return nil, err
	}
	if applyExists {
		return nil, ErrApplyAlreadyExists
	}

	// 创建好友申请
	apply := f.newApply(roleID, friendID, req.Source)

	// 保存申请
	_, err = gxymongo.Client().InsertOne(ctx, FriendRelationCol, apply)
	if err != nil {
		glog.Errorf(ctx, "insert friend apply failed: %v, roleID: %s, friendID: %s", err, roleID, friendID)
		return nil, err
	}

	// 获取申请者的公开信息
	// 这里需要调用角色服务获取角色公开信息
	// 暂时返回空的公开信息
	return &api.ApplyFriendRes{ApplyNew: *apply}, nil
}

func (f *FriendController) newApply(roleID int64, friendID int64, source string) *api.FriendInfo {
	return &api.FriendInfo{
		RoleID:     roleID,
		FriendID:   friendID,
		Source:     source,
		FriendTime: time.Now().Unix(),
		State:      FRIENND_STATE_APPLY,
	}
}

// DealApply 处理好友申请
func (f *FriendController) DealApply(ctx context.Context, req *api.DealApplyReq) (*api.DealApplyRes, error) {
	// 查找待处理的申请
	filter := bson.M{
		"role_id":   req.ApplyerID,
		"friend_id": req.RoleID,
		"state":     FRIENND_STATE_APPLY,
	}

	var apply api.FriendInfo
	err := gxymongo.Client().FindOne(ctx, &apply, FriendRelationCol, filter)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return nil, ErrApplyNotFound
		}
		return nil, err
	}

	// 如果接受申请，创建好友关系
	rsp := &api.DealApplyRes{
		ApplyDelete: apply,
	}

	// 使用事务处理接受申请的逻辑，确保原子性
	if req.Deal == 1 {
		// 为双方创建好友关系
		friendInfo1 := f.newFriend(req.ApplyerID, req.RoleID, apply.Source)
		friendInfo2 := f.newFriend(req.RoleID, req.ApplyerID, apply.Source)

		// 使用事务保证原子性
		_, err = gxymongo.Client().WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
			// 1. 删除原来的申请记录
			_, err := gxymongo.Client().DeleteOne(sessCtx, FriendRelationCol, filter)
			if err != nil {
				glog.Errorf(ctx, "delete friend apply failed: %v, roleID: %d, friendID: %d", err, req.ApplyerID, req.RoleID)
				return nil, err
			}

			// 2. 插入双方好友关系
			_, err = gxymongo.Client().InsertOne(sessCtx, FriendRelationCol, friendInfo1)
			if err != nil {
				glog.Errorf(ctx, "insert friend info failed: %v, friend: %s", err, util.FormatObject(friendInfo1))
				return nil, err
			}

			result, err := gxymongo.Client().InsertOne(sessCtx, FriendRelationCol, friendInfo2)
			if err != nil {
				glog.Errorf(ctx, "insert friend info failed: %v, friend: %s", err, util.FormatObject(friendInfo2))
				return nil, err
			}

			return result, nil
		})

		if err != nil {
			glog.Errorf(ctx, "deal friend apply transaction failed: %v", err)
			return nil, err
		}

		rsp.FriendNew = *friendInfo2
	} else {
		// 拒绝申请，删除申请记录
		_, err = gxymongo.Client().DeleteOne(ctx, FriendRelationCol, filter)
		if err != nil {
			glog.Errorf(ctx, "delete friend apply failed: %v, roleID: %d, friendID: %d", err, req.ApplyerID, req.RoleID)
			return nil, err
		}
	}

	return rsp, nil
}

func (f *FriendController) newFriend(roleID int64, friendID int64, source string) *api.FriendInfo {
	return &api.FriendInfo{
		RoleID:     roleID,
		FriendID:   friendID,
		Source:     source,
		FriendTime: time.Now().Unix(),
		State:      FRIENND_STATE_FRIEND,
	}
}

// Delete 删除好友
func (f *FriendController) Delete(ctx context.Context, req *api.DeleteFriendReq) (*api.DeleteFriendRes, error) {
	// 检查是否是好友
	isFriend, err := f.isFriend(ctx, req.RoleID, req.FriendID)
	if err != nil {
		return nil, err
	}
	if !isFriend {
		return nil, ErrNotFriend
	}

	// 从双方的好友列表中删除
	filter1 := bson.M{"role_id": req.RoleID, "friend_id": req.FriendID}
	filter2 := bson.M{"role_id": req.FriendID, "friend_id": req.RoleID}

	// 使用事务保证原子性
	_, err = gxymongo.Client().WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
		_, terr := gxymongo.Client().DeleteOne(sessCtx, FriendRelationCol, filter1)
		if terr != nil {
			glog.Errorf(ctx, "delete friend from role %d failed: %v", req.RoleID, err)
			return nil, terr
		}

		result, terr := gxymongo.Client().DeleteOne(sessCtx, FriendRelationCol, filter2)
		if terr != nil {
			glog.Errorf(ctx, "delete friend from role %d failed: %v", req.FriendID, terr)
			return nil, terr
		}

		return result, nil
	})

	if err != nil {
		glog.Errorf(ctx, "delete friend transaction failed: %v", err)
		return nil, err
	}

	return &api.DeleteFriendRes{}, nil
}

// GetFriendList 获取好友列表
func (f *FriendController) GetFriendList(ctx context.Context, req *api.GetFriendListReq) (*api.GetFriendListRes, error) {
	// 查询用户的所有好友
	filter := bson.M{"role_id": req.RoleID}

	var friendInfos []api.FriendInfo
	err := gxymongo.Client().Find(ctx, &friendInfos, FriendRelationCol, filter)
	if err != nil {
		glog.Errorf(ctx, "get friend list for role %s failed: %v", req.RoleID, err)
		return nil, err
	}

	// 构建返回结果
	rsp := &api.GetFriendListRes{
		FriendList:    make([]api.FriendInfo, 0, len(friendInfos)),
		ApplySendList: make([]api.FriendInfo, 0, len(friendInfos)),
		ApplyRecvList: make([]api.FriendInfo, 0, len(friendInfos)),
	}

	for _, info := range friendInfos {
		// 这里应该从角色服务获取好友的公开信息
		// 暂时创建空的公开信息
		if info.State == FRIENND_STATE_APPLY {
			if info.RoleID == req.RoleID {
				rsp.ApplySendList = append(rsp.ApplySendList, info)
			} else {
				rsp.ApplyRecvList = append(rsp.ApplyRecvList, info)
			}
		} else {
			rsp.FriendList = append(rsp.FriendList, info)
		}
	}

	return rsp, nil
}

// 辅助方法
// isFriend 检查两个角色是否是好友
func (f *FriendController) isFriend(ctx context.Context, roleID int64, friendID int64) (bool, error) {
	filter := bson.M{
		"role_id":   roleID,
		"friend_id": friendID,
		"state":     FRIENND_STATE_FRIEND,
	}

	var friendInfo api.FriendInfo
	err := gxymongo.Client().FindOne(ctx, &friendInfo, FriendRelationCol, filter)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// applyExists 检查好友申请是否已存在
func (f *FriendController) applyExists(ctx context.Context, ApplyerID int64, targetID int64) (bool, error) {
	filter := bson.M{
		"role_id":   ApplyerID,
		"friend_id": targetID,
		"state":     FRIENND_STATE_APPLY,
	}

	var friendInfo api.FriendInfo
	err := gxymongo.Client().FindOne(ctx, &friendInfo, FriendRelationCol, filter)
	if err != nil {
		if err == mongo.ErrNoDocuments {
			return false, nil
		}
		return false, err
	}

	return true, nil
}

// 错误定义
var (
	ErrAlreadyFriend      = gxyhttp.NewErrCode(1001, "already a friend")
	ErrApplyAlreadyExists = gxyhttp.NewErrCode(1002, "friend apply already exists")
	ErrApplyNotFound      = gxyhttp.NewErrCode(1003, "friend apply not found")
	ErrDeleteFriendFailed = gxyhttp.NewErrCode(1004, "delete friend failed")
	ErrNotFriend          = gxyhttp.NewErrCode(1005, "not a friend")
)
