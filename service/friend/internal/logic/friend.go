package logic

import (
	"context"
	"fmt"
	"time"

	"gserver/core/gxyhttp"
	"gserver/core/gxymongo"
	"gserver/core/gxyredis"
	"gserver/service/api"
	"gserver/service/push"
	"gserver/util"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/redis/go-redis/v9"
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
	FRIENND_STATE_APPLY   = 1 // 申请中
	FRIENND_STATE_APPLIED = 2 // 已申请
	FRIENND_STATE_FRIEND  = 3 // 好友
)

type friendInfoList struct {
	FriendList []api.FriendInfo `json:"friend_list"`
}

func (f *friendInfoList) getFriendInfo(friendID int64) *api.FriendInfo {
	for _, friend := range f.FriendList {
		if friend.FriendID == friendID {
			return &friend
		}
	}
	return nil
}

func (f *friendInfoList) isFriend(friendID int64) bool {
	info := f.getFriendInfo(friendID)
	return info != nil && info.State == FRIENND_STATE_FRIEND
}

func (f *friendInfoList) isApplySend(friendID int64) bool {
	info := f.getFriendInfo(friendID)
	return info != nil && info.State == FRIENND_STATE_APPLY
}

func getFriendCacheKey(roleID int64) string {
	return fmt.Sprintf("friend_data:%d", roleID)
}

func (f *FriendController) getFriendInfoList(ctx context.Context, roleID int64) (*friendInfoList, error) {
	cacheKey := getFriendCacheKey(roleID)
	rd := gxyredis.GetRedis()
	friendInfoList := &friendInfoList{}
	friendMap, err := rd.HGetAll(ctx, cacheKey).Result()
	if err != nil {
		if err == redis.Nil {
			friendList, err := f.getFriendInfoListFromDB(ctx, roleID)
			if err != nil {
				return nil, err
			}
			kvList := []any{}
			for _, friend := range *friendList {
				kvList = append(kvList, fmt.Sprintf("%d", friend.FriendID), gjson.MustEncodeString(friend))
			}
			// 使用 HMSet 一次性写入
			_, err = rd.HMSet(ctx, cacheKey, kvList...).Result()
			rd.Expire(ctx, cacheKey, time.Hour)
			if err != nil {
				glog.Errorf(ctx, "HMSet friend data failed: %v", err)
				return nil, err
			}
			friendInfoList.FriendList = *friendList
		} else {
			return nil, err
		}
	} else {
		for friendID, friendStr := range friendMap {
			friendInfo := api.FriendInfo{}
			err := gjson.Unmarshal([]byte(friendStr), &friendInfo)
			if err != nil {
				glog.Errorf(ctx, "unmarshal friend info failed: %v, friendID: %d", err, friendID)
				continue
			}
			friendInfoList.FriendList = append(friendInfoList.FriendList, friendInfo)
		}
	}
	return friendInfoList, nil
}

func (f *FriendController) getFriendInfoListFromDB(ctx context.Context, roleID int64) (*[]api.FriendInfo, error) {
	filter := bson.M{"role_id": roleID}

	var friendInfos []api.FriendInfo
	err := gxymongo.Client().Find(ctx, &friendInfos, FriendRelationCol, filter)
	if err != nil {
		glog.Errorf(ctx, "get friend list for role %d failed: %v", roleID, err)
		return nil, err
	}
	return &friendInfos, nil
}

// Apply 申请添加好友
func (f *FriendController) Apply(ctx context.Context, req *api.ApplyFriendReq) (*api.ApplyFriendRes, error) {
	roleID := req.RoleID
	friendID := req.FriendID

	friendInfoList, err := f.getFriendInfoList(ctx, roleID)
	if err != nil {
		return nil, err
	}
	// 检查是否已经是好友
	isFriend := friendInfoList.isFriend(friendID)
	if isFriend {
		return nil, ErrAlreadyFriend
	}

	// 检查是否已经发送过申请
	applySended := friendInfoList.isApplySend(friendID)
	if applySended {
		return nil, ErrApplyAlreadyExists
	}

	// 创建好友申请
	apply_send := f.newApply(roleID, friendID, req.Source, FRIENND_STATE_APPLY)
	apply_recv := f.newApply(friendID, roleID, req.Source, FRIENND_STATE_APPLIED)

	// 保存申请
	// 1. 保存发送申请
	_, err = gxymongo.Client().WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
		_, err = gxymongo.Client().InsertOne(sessCtx, FriendRelationCol, apply_send)
		if err != nil {
			glog.Errorf(ctx, "insert friend apply failed: %v, roleID: %d, friendID: %d", err, roleID, friendID)
			return nil, err
		}
		// 2. 保存接收申请
		_, err = gxymongo.Client().InsertOne(sessCtx, FriendRelationCol, apply_recv)
		if err != nil {
			glog.Errorf(ctx, "insert friend apply failed: %v, roleID: %d, friendID: %d", err, roleID, friendID)
			return nil, err
		}
		if err != nil {
			glog.Errorf(ctx, "insert friend apply failed: %v, roleID: %d, friendID: %d", err, roleID, friendID)
			return nil, err
		}
		return nil, nil
	})
	if err != nil {
		return nil, err
	}
	f.updateCache(ctx, apply_send)
	f.updateCache(ctx, apply_recv)
	// 3. 通知好友
	f.notifyFriend(ctx, friendID, roleID, api.FriendNotifyTypeApplyRecv)

	return &api.ApplyFriendRes{ApplyNew: *apply_send}, nil
}

func (f *FriendController) newApply(roleID int64, friendID int64, source string, applyState int) *api.FriendInfo {
	return &api.FriendInfo{
		RoleID:     roleID,
		FriendID:   friendID,
		Source:     source,
		FriendTime: time.Now(),
		State:      int32(applyState),
	}
}

// DealApply 处理好友申请
func (f *FriendController) DealApply(ctx context.Context, req *api.DealApplyReq) (*api.DealApplyRes, error) {
	// 查找待处理的申请
	applyfilter := bson.M{
		"role_id":   req.ApplyerID,
		"friend_id": req.RoleID,
		"state":     FRIENND_STATE_APPLY,
	}

	applyedfilter := bson.M{
		"role_id":   req.RoleID,
		"friend_id": req.ApplyerID,
		"state":     FRIENND_STATE_APPLIED,
	}

	var apply *api.FriendInfo
	roleFrdList, err := f.getFriendInfoList(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	apply = roleFrdList.getFriendInfo(req.ApplyerID)
	if apply == nil {
		return nil, ErrApplyNotFound
	}

	// 如果接受申请，创建好友关系
	rsp := &api.DealApplyRes{
		ApplyDelete: *apply,
	}

	// 使用事务处理接受申请的逻辑，确保原子性
	if req.Deal == 1 {

		// 使用事务保证原子性
		_, err = gxymongo.Client().WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
			// 2. 插入双方好友关系
			_, err = gxymongo.Client().UpdateOne(sessCtx, FriendRelationCol, applyfilter, bson.M{"$set": bson.M{"state": FRIENND_STATE_FRIEND}})
			if err != nil {
				glog.Errorf(ctx, "udpate friend apply state failed: %v, filter: %s", err, util.FormatObject(applyfilter))
				return nil, err
			}

			_, err = gxymongo.Client().UpdateOne(sessCtx, FriendRelationCol, applyedfilter, bson.M{"$set": bson.M{"state": FRIENND_STATE_FRIEND}})
			if err != nil {
				glog.Errorf(ctx, "udpate friend apply state failed: %v, filter: %s", err, util.FormatObject(applyedfilter))
				return nil, err
			}

			return nil, nil
		})

		if err != nil {
			glog.Errorf(ctx, "deal friend apply transaction failed: %v", err)
			return nil, err
		}
		f.updateCache(ctx, apply)
		// 4. 通知好友
		f.notifyFriend(ctx, req.ApplyerID, req.RoleID, api.FriendNotifyTypeAdd)
		rsp.FriendNew = *f.newFriend(req.ApplyerID, req.RoleID, apply.Source)
	} else {
		// 拒绝申请，删除申请记录
		_, err = gxymongo.Client().WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
			_, err = gxymongo.Client().DeleteOne(sessCtx, FriendRelationCol, applyfilter)
			if err != nil {
				glog.Errorf(ctx, "delete friend apply failed: %v, filter: %s", err, util.FormatObject(applyfilter))
				return nil, err
			}

			_, err = gxymongo.Client().DeleteOne(sessCtx, FriendRelationCol, applyedfilter)
			if err != nil {
				glog.Errorf(ctx, "delete friend apply failed: %v, filter: %s", err, util.FormatObject(applyedfilter))
				return nil, err
			}

			return nil, nil
		})
		if err != nil {
			glog.Errorf(ctx, "delete friend apply transaction failed: %v", err)
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
		FriendTime: time.Now(),
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
	f.notifyFriend(ctx, req.FriendID, req.RoleID, api.FriendNotifyTypeDel)

	return &api.DeleteFriendRes{}, nil
}

// GetFriendList 获取好友列表
func (f *FriendController) GetFriendList(ctx context.Context, req *api.GetFriendListReq) (*api.GetFriendListRes, error) {
	// 查询用户的所有好友
	filter := bson.M{"role_id": req.RoleID}

	var friendInfos []api.FriendInfo
	err := gxymongo.Client().Find(ctx, &friendInfos, FriendRelationCol, filter)
	if err != nil {
		glog.Errorf(ctx, "get friend list for role %d failed: %v", req.RoleID, err)
		return nil, err
	}

	// 构建返回结果
	rsp := &api.GetFriendListRes{
		FriendList:    make([]api.FriendInfo, 0, len(friendInfos)),
		ApplySendList: make([]api.FriendInfo, 0, len(friendInfos)),
		ApplyRecvList: make([]api.FriendInfo, 0, len(friendInfos)),
	}

	for _, info := range friendInfos {
		switch info.State {
		case FRIENND_STATE_APPLY:
			rsp.ApplySendList = append(rsp.ApplySendList, info)
		case FRIENND_STATE_APPLIED:
			rsp.ApplyRecvList = append(rsp.ApplyRecvList, info)
		case FRIENND_STATE_FRIEND:
			rsp.FriendList = append(rsp.FriendList, info)
		default:
			glog.Warningf(ctx, "unknown friend state %d for role %d", info.State, req.RoleID)
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

func (r *FriendController) notifyFriend(ctx context.Context, roleID int64, friendID int64, notifyType int32) {
	push.PushService().NotifyRoleMessageOnline(ctx, roleID, &api.FriendNotify{
		NotifyList: []api.PFriendNotify{
			{
				Type:     notifyType,
				FriendID: friendID,
			},
		},
	})
}

func (r *FriendController) updateCache(ctx context.Context, frdInfo *api.FriendInfo) {
	key := getFriendCacheKey(frdInfo.RoleID)
	_, err := gxyredis.GetRedis().Pipelined(ctx, func(pipe redis.Pipeliner) error {
		ex, _ := pipe.Exists(ctx, key).Uint64()
		if ex == 1 {
			return pipe.HSet(ctx, key, frdInfo.FriendID, gjson.MustEncodeString(frdInfo)).Err()
		}
		return nil
	})
	if err != nil {
		glog.Errorf(ctx, "update friend cache failed: %v, roleID: %d, friendID: %d", err, frdInfo.RoleID, frdInfo.FriendID)
		r.clearCache(ctx, frdInfo.RoleID)
	}
}

func (r *FriendController) clearCache(ctx context.Context, roleID int64) {
	key := getFriendCacheKey(roleID)
	gxyredis.GetRedis().Del(ctx, key)
}

// 错误定义
var (
	ErrAlreadyFriend      = gxyhttp.NewErrCode(1001, "already a friend")
	ErrApplyAlreadyExists = gxyhttp.NewErrCode(1002, "friend apply already exists")
	ErrApplyNotFound      = gxyhttp.NewErrCode(1003, "friend apply not found")
	ErrDeleteFriendFailed = gxyhttp.NewErrCode(1004, "delete friend failed")
	ErrNotFriend          = gxyhttp.NewErrCode(1005, "not a friend")
)
