package logic

import (
	"context"
	"fmt"
	"time"

	"gserver/apps/api"
	"gserver/core/gxyhttp"
	"gserver/core/gxymongo"
	"gserver/core/gxyredis"
	"gserver/lib"
	"gserver/util"

	"github.com/gogf/gf/v2/encoding/gjson"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/redis/go-redis/v9"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

const (
	FRIEND_CACHE_EXPIRE      = time.Hour
	FRIEND_CACHE_PLACEHOLDER = "placeholder"
)

const (
	MAX_FRIEND_COUNT = 100
	MAX_APPLY_SEND   = 150
	MAX_APPLY_RECV   = 150
)

// FriendServer 好友控制器
type FriendServer struct {
}

const (
	// 集合名称
	FriendRelationCol = "friend_relation"
)

const (
	FRIEND_STATE_APPLY   = 1 // 申请中
	FRIEND_STATE_APPLIED = 2 // 已申请
	FRIEND_STATE_FRIEND  = 3 // 好友
)

type friendData struct {
	FriendList    map[int64]*api.FriendInfo `json:"friend_list"`
	ApplySendList map[int64]*api.FriendInfo `json:"apply_send_list"`
	ApplyRecvList map[int64]*api.FriendInfo `json:"apply_recv_list"`
}

func (f *friendData) isFriend(friendID int64) bool {
	if _, ok := f.FriendList[friendID]; ok {
		return true
	}
	return false
}

func (f *friendData) isApplySend(friendID int64) bool {
	if _, ok := f.ApplySendList[friendID]; ok {
		return true
	}
	return false
}

func (f *friendData) isApplyRecv(friendID int64) bool {
	if _, ok := f.ApplyRecvList[friendID]; ok {
		return true
	}
	return false
}

func getFriendCacheKey(roleID int64) string {
	return fmt.Sprintf("friend_data:%d", roleID)
}

// NewFriendController 创建好友控制器实例
func NewFriendServer() *FriendServer {
	server := &FriendServer{}
	return server
}

func (f *FriendServer) getFriendData(ctx context.Context, roleID int64) (*friendData, error) {
	cacheKey := getFriendCacheKey(roleID)
	rd := gxyredis.Redis()

	friendInfoList := []*api.FriendInfo{}
	friendMap, err := rd.HGetAll(ctx, cacheKey).Result()
	if err != nil {
		return nil, err
	}

	// Redis HGetAll 在 key 不存在时返回空 map 而不是错误
	// 所以需要通过检查 map 长度来判断缓存是否有效
	if len(friendMap) == 0 {
		friendList, err := f.getFriendInfoListFromDB(ctx, roleID)
		if err != nil {
			return nil, err
		}
		// time占位，防止玩家列表为空时，没有缓存
		kvList := []any{FRIEND_CACHE_PLACEHOLDER, 1}
		for _, friend := range *friendList {
			kvList = append(kvList, fmt.Sprintf("%d", friend.FriendID), gjson.MustEncodeString(friend))
			friendInfoList = append(friendInfoList, &friend)
		}
		// 使用 HMSet 一次性写入
		_, err = rd.HMSet(ctx, cacheKey, kvList...).Result()
		rd.Expire(ctx, cacheKey, FRIEND_CACHE_EXPIRE)
		if err != nil {
			glog.Errorf(ctx, "HMSet friend data failed: %v", err)
			return nil, err
		}
	} else {
		for friendID, friendStr := range friendMap {
			if friendID == FRIEND_CACHE_PLACEHOLDER {
				continue
			}
			friendInfo := api.FriendInfo{}
			err := gjson.Unmarshal([]byte(friendStr), &friendInfo)
			if err != nil {
				glog.Errorf(ctx, "unmarshal friend info failed: %v, friendID: %s", err, friendID)
				continue
			}
			friendInfoList = append(friendInfoList, &friendInfo)
		}
	}
	friendData := &friendData{
		FriendList:    map[int64]*api.FriendInfo{},
		ApplySendList: map[int64]*api.FriendInfo{},
		ApplyRecvList: map[int64]*api.FriendInfo{},
	}
	for _, friend := range friendInfoList {
		switch friend.State {
		case FRIEND_STATE_FRIEND:
			friendData.FriendList[friend.FriendID] = friend
		case FRIEND_STATE_APPLY:
			friendData.ApplySendList[friend.FriendID] = friend
		case FRIEND_STATE_APPLIED:
			friendData.ApplyRecvList[friend.FriendID] = friend
		}
	}
	return friendData, nil
}

func (f *FriendServer) getFriendInfoListFromDB(ctx context.Context, roleID int64) (*[]api.FriendInfo, error) {
	filter := bson.M{"role_id": roleID}

	var friendInfos []api.FriendInfo
	err := gxymongo.Mongo().Find(ctx, &friendInfos, FriendRelationCol, filter)
	if err != nil {
		glog.Errorf(ctx, "get friend list for role %d failed: %v", roleID, err)
		return nil, err
	}
	return &friendInfos, nil
}

// Apply 申请添加好友
func (f *FriendServer) Apply(ctx context.Context, req *api.ApplyFriendReq) (*api.ApplyFriendRes, error) {
	roleID := req.RoleID
	friendID := req.FriendID
	if roleID == friendID {
		return nil, ErrApplySelf
	}

	roleFriendData, err := f.getFriendData(ctx, roleID)
	if err != nil {
		return nil, err
	}
	// 检查是否已经是好友
	isFriend := roleFriendData.isFriend(friendID)
	if isFriend {
		return nil, ErrAlreadyFriend
	}

	// 检查是否已经发送过申请
	applySended := roleFriendData.isApplySend(friendID)
	if applySended {
		return nil, ErrApplyAlreadyExists
	}
	if len(roleFriendData.ApplySendList) >= MAX_APPLY_SEND {
		return nil, ErrApplySendMax
	}

	if len(roleFriendData.FriendList) >= MAX_FRIEND_COUNT {
		return nil, ErrFriendFull
	}

	applyer, err := f.getFriendData(ctx, friendID)
	if err != nil {
		return nil, err
	}
	// 对方申请列表是否已满
	if len(applyer.ApplyRecvList) >= MAX_APPLY_RECV {
		return nil, ErrApplyRecvMax
	}

	// 创建好友申请
	apply_send := f.newApply(roleID, friendID, req.Source, FRIEND_STATE_APPLY)
	apply_recv := f.newApply(friendID, roleID, req.Source, FRIEND_STATE_APPLIED)

	// 保存申请
	// 1. 保存发送申请
	_, err = gxymongo.Mongo().WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
		_, err = gxymongo.Mongo().InsertOne(sessCtx, FriendRelationCol, apply_send)
		if err != nil {
			glog.Errorf(ctx, "insert friend apply failed: %v, roleID: %d, friendID: %d", err, roleID, friendID)
			return nil, err
		}
		// 2. 保存接收申请
		_, err = gxymongo.Mongo().InsertOne(sessCtx, FriendRelationCol, apply_recv)
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
	f.notifyFriend(ctx, friendID, &api.FriendNotify{
		ApplyRecvAddList: []api.FriendInfo{*apply_recv},
	})

	return &api.ApplyFriendRes{ApplyNew: *apply_send}, nil
}

func (f *FriendServer) newApply(roleID int64, friendID int64, source string, applyState int) *api.FriendInfo {
	return &api.FriendInfo{
		RoleID:     roleID,
		FriendID:   friendID,
		Source:     source,
		FriendTime: time.Now(),
		State:      int32(applyState),
	}
}

// DealApply 处理好友申请
func (f *FriendServer) DealApply(ctx context.Context, req *api.DealApplyReq) (*api.DealApplyRes, error) {
	// 查找待处理的申请
	applyfilter := bson.M{
		"role_id":   req.ApplyerID,
		"friend_id": req.RoleID,
		"state":     FRIEND_STATE_APPLY,
	}

	applyedfilter := bson.M{
		"role_id":   req.RoleID,
		"friend_id": req.ApplyerID,
		"state":     FRIEND_STATE_APPLIED,
	}

	roleFrdData, err := f.getFriendData(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}

	applyRecv := roleFrdData.ApplyRecvList[req.ApplyerID]
	if applyRecv == nil {
		return nil, ErrApplyNotFound
	}

	// 如果接受申请，创建好友关系
	rsp := &api.DealApplyRes{
		ApplyDelete: *applyRecv,
	}

	applyer, err := f.getFriendData(ctx, req.ApplyerID)
	if err != nil {
		return nil, err
	}
	applySend := applyer.ApplySendList[req.RoleID]
	if applySend == nil {
		return nil, ErrApplyNotFound
	}
	// 使用事务处理接受申请的逻辑，确保原子性
	if req.Deal == 1 {
		// 使用事务保证原子性
		_, err = gxymongo.Mongo().WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
			// 2. 插入双方好友关系
			_, err = gxymongo.Mongo().UpdateOne(sessCtx, FriendRelationCol, applyfilter, bson.M{"$set": bson.M{"state": FRIEND_STATE_FRIEND}})
			if err != nil {
				glog.Errorf(ctx, "udpate friend apply state failed: %v, filter: %s", err, util.FormatObject(applyfilter))
				return nil, err
			}

			_, err = gxymongo.Mongo().UpdateOne(sessCtx, FriendRelationCol, applyedfilter, bson.M{"$set": bson.M{"state": FRIEND_STATE_FRIEND}})
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
		applyRecv.State = FRIEND_STATE_FRIEND
		f.updateCache(ctx, applyRecv)
		applySend.State = FRIEND_STATE_FRIEND
		f.updateCache(ctx, applySend)
		// 4. 通知好友
		f.notifyFriend(ctx, req.ApplyerID, &api.FriendNotify{
			FriendAddList: []api.FriendInfo{*applyRecv},
		})
		rsp.FriendNew = *f.newFriend(req.ApplyerID, req.RoleID, applyRecv.Source)
	} else {
		// 拒绝申请，删除申请记录
		_, err = gxymongo.Mongo().WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
			_, err = gxymongo.Mongo().DeleteOne(sessCtx, FriendRelationCol, applyfilter)
			if err != nil {
				glog.Errorf(ctx, "delete friend apply failed: %v, filter: %s", err, util.FormatObject(applyfilter))
				return nil, err
			}

			_, err = gxymongo.Mongo().DeleteOne(sessCtx, FriendRelationCol, applyedfilter)
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
		f.deleteCache(ctx, req.ApplyerID, req.RoleID)
		f.deleteCache(ctx, req.RoleID, req.ApplyerID)
	}

	return rsp, nil
}

func (f *FriendServer) newFriend(roleID int64, friendID int64, source string) *api.FriendInfo {
	return &api.FriendInfo{
		RoleID:     roleID,
		FriendID:   friendID,
		Source:     source,
		FriendTime: time.Now(),
		State:      FRIEND_STATE_FRIEND,
	}
}

// Delete 删除好友
func (f *FriendServer) Delete(ctx context.Context, req *api.DeleteFriendReq) (*api.DeleteFriendRes, error) {
	// 检查是否是好友
	friendData, err := f.getFriendData(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	if !friendData.isFriend(req.FriendID) {
		return nil, ErrNotFriend
	}
	// 从双方的好友列表中删除
	filter1 := bson.M{"role_id": req.RoleID, "friend_id": req.FriendID}
	filter2 := bson.M{"role_id": req.FriendID, "friend_id": req.RoleID}

	// 使用事务保证原子性
	_, err = gxymongo.Mongo().WithTransaction(ctx, func(sessCtx mongo.SessionContext) (any, error) {
		_, terr := gxymongo.Mongo().DeleteOne(sessCtx, FriendRelationCol, filter1)
		if terr != nil {
			glog.Errorf(ctx, "delete friend from role %d failed: %v", req.RoleID, err)
			return nil, terr
		}

		result, terr := gxymongo.Mongo().DeleteOne(sessCtx, FriendRelationCol, filter2)
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
	// 删除缓存
	f.deleteCache(ctx, req.RoleID, req.FriendID)
	f.deleteCache(ctx, req.FriendID, req.RoleID)

	f.notifyFriend(ctx, req.FriendID, &api.FriendNotify{
		FriendDeleteList: []int64{req.RoleID},
	})

	return &api.DeleteFriendRes{}, nil
}

// GetFriendList 获取好友列表
func (f *FriendServer) GetFriendList(ctx context.Context, req *api.GetFriendListReq) (*api.GetFriendListRes, error) {
	// 查询用户的所有好友
	friendData, err := f.getFriendData(ctx, req.RoleID)
	if err != nil {
		return nil, err
	}
	// 构建返回结果
	data := &api.FriendData{
		FriendList:    make([]api.FriendInfo, 0, len(friendData.FriendList)),
		ApplySendList: make([]api.FriendInfo, 0, len(friendData.ApplySendList)),
		ApplyRecvList: make([]api.FriendInfo, 0, len(friendData.ApplyRecvList)),
	}

	for _, info := range friendData.FriendList {
		data.FriendList = append(data.FriendList, *info)
	}
	for _, info := range friendData.ApplySendList {
		data.ApplySendList = append(data.ApplySendList, *info)
	}
	for _, info := range friendData.ApplyRecvList {
		data.ApplyRecvList = append(data.ApplyRecvList, *info)
	}

	return &api.GetFriendListRes{FriendData: *data}, nil
}

func (f *FriendServer) notifyFriend(ctx context.Context, roleID int64, notify *api.FriendNotify) {
	lib.NotifyRoleMessageOnline(ctx, roleID, notify)
}

func (f *FriendServer) updateCache(ctx context.Context, frdInfo *api.FriendInfo) {
	key := getFriendCacheKey(frdInfo.RoleID)
	_, err := gxyredis.Redis().TxPipelined(ctx, func(pipe redis.Pipeliner) error {
		if err := pipe.HSet(ctx, key, frdInfo.FriendID, gjson.MustEncodeString(frdInfo)).Err(); err != nil {
			return err
		}
		if err := pipe.Expire(ctx, key, FRIEND_CACHE_EXPIRE).Err(); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		glog.Errorf(ctx, "update friend cache failed: %v, roleID: %d, friendID: %d", err, frdInfo.RoleID, frdInfo.FriendID)
		f.clearCache(ctx, frdInfo.RoleID)
	}
}

func (f *FriendServer) deleteCache(ctx context.Context, roleID int64, friendID int64) {
	key := getFriendCacheKey(roleID)
	gxyredis.Redis().HDel(ctx, key, fmt.Sprintf("%d", friendID))
}

func (f *FriendServer) clearCache(ctx context.Context, roleID int64) {
	key := getFriendCacheKey(roleID)
	gxyredis.Redis().Del(ctx, key)
}

// 错误定义
var (
	ErrAlreadyFriend      = gxyhttp.NewErrCode(1001, "already a friend")
	ErrApplyAlreadyExists = gxyhttp.NewErrCode(1002, "friend apply already exists")
	ErrApplyNotFound      = gxyhttp.NewErrCode(1003, "friend apply not found")
	ErrDeleteFriendFailed = gxyhttp.NewErrCode(1004, "delete friend failed")
	ErrNotFriend          = gxyhttp.NewErrCode(1005, "not a friend")
	ErrApplySendMax       = gxyhttp.NewErrCode(1006, "apply send full")
	ErrFriendFull         = gxyhttp.NewErrCode(1007, "friend full")
	ErrApplyRecvMax       = gxyhttp.NewErrCode(1008, "apply recv full")
	ErrApplySelf          = gxyhttp.NewErrCode(1009, "apply self")
)
