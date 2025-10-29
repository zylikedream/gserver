package logic

import (
	"context"
	"gserver/protocol/pb"
	"gserver/service/friend"
	"gserver/service/friend/api"
	"slices"
	"time"

	"github.com/gogf/gf/v2/errors/gerror"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/gogf/gf/v2/util/gconv"
)

const (
	RoleFriendCacheExpire = 2 * time.Hour
)

type FriendData struct {
	FriendID     int64     `bson:"friend_id"`
	SendGiftTime time.Time `bson:"send_gift_time"`
	RecvGiftTime time.Time `bson:"recv_gift_time"`
	Favor        int       `bson:"favor"`
}

type RoleFriendState struct {
	RolePersistState `bson:"inline"`
	FriendDataList   map[int64]FriendData `bson:"friend_data_list"`
}

type friendCache struct {
	updateTime    time.Time
	friendList    []api.FriendInfo
	applySendList []api.FriendInfo
	applyRecvList []api.FriendInfo
}

type RoleFriend struct {
	RoleModule `bson:"inline"`
	RoleFriendState
	cache friendCache
}

var _ IRoleModule = (*RoleFriend)(nil)

func (r *RoleFriend) OnModInit(ctx context.Context) error {
	r.FriendDataList = make(map[int64]FriendData)
	return nil
}

func (r *RoleFriend) PersistState() IPersistState {
	return &r.RoleFriendState
}

func (r *RoleFriend) ensureFriendInfo(ctx context.Context) (*friendCache, error) {
	if !r.cache.updateTime.IsZero() && time.Since(r.cache.updateTime) < RoleFriendCacheExpire {
		return &r.cache, nil
	}
	FriendInfo, err := friend.FriendService().GetFriendInfo(ctx, r.RoleID)
	if err != nil {
		glog.Errorf(ctx, "get friend info failed, roleID: %d, err: %+v", r.RoleID, err)
		return &r.cache, gerror.Wrapf(err, "get friend info failed")
	}
	r.cache.friendList = FriendInfo.FriendList
	r.cache.applySendList = FriendInfo.ApplySendList
	r.cache.applyRecvList = FriendInfo.ApplyRecvList
	r.cache.updateTime = time.Now()
	return &r.cache, nil

}

func (r *RoleFriend) ReqFriendInfo(ctx context.Context, req *pb.ReqFriendInfo) (*pb.RspFriendInfo, error) {

	FriendInfo, err := r.ensureFriendInfo(ctx)
	if err != nil {
		return nil, err
	}
	rsp := &pb.RspFriendInfo{}
	for _, friend := range FriendInfo.friendList {
		rsp.Friends = append(rsp.Friends, r.packFriendInfo(ctx, &friend))
	}
	for _, apply := range FriendInfo.applySendList {
		rsp.ApplySend = append(rsp.ApplySend, &pb.PFriendApply{
			Base: r.packFriendBase(ctx, &apply),
		})
	}
	for _, apply := range FriendInfo.applyRecvList {
		rsp.ApplyRecv = append(rsp.ApplyRecv, &pb.PFriendApply{
			Base: r.packFriendBase(ctx, &apply),
		})
	}

	return rsp, nil
}

func (r *RoleFriend) getFriendData(friendID int64) FriendData {
	return r.FriendDataList[friendID]
}

func (r *RoleFriend) packFriendBase(ctx context.Context, frd *api.FriendInfo) *pb.PFriendBase {
	friendID := gconv.Int64(frd.FriendID)
	friendPublic := GetRolePublic(ctx, friendID)
	return &pb.PFriendBase{
		RolePublic: friendPublic,
		FriendTime: frd.FriendTime,
		Source:     frd.Source,
	}
}

func (r *RoleFriend) packFriendInfo(ctx context.Context, frd *api.FriendInfo) *pb.PFriendInfo {
	friendData := r.getFriendData(frd.FriendID)
	return &pb.PFriendInfo{
		Base:         r.packFriendBase(ctx, frd),
		SendGiftTime: friendData.SendGiftTime.Unix(),
		CanRecvGift:  false,
		Favor:        int32(friendData.Favor),
	}
}

func (r *RoleFriend) ReqFriendApply(ctx context.Context, req *pb.ReqFriendApply) (*pb.RspFriendApply, error) {
	if len(req.ApplyList) == 0 {
		return nil, gerror.New("apply list is empty")
	}
	var errs []error
	var newApply []*api.FriendInfo
	for _, apply := range req.ApplyList {
		friendID := gconv.Int64(apply.RoleId)
		if friendInfo, err := r.applyFriendSingle(ctx, friendID, apply.Source); err != nil {
			errs = append(errs, err)
		} else {
			newApply = append(newApply, friendInfo)
		}
	}
	// 一个都没有成功，返回错误
	if len(errs) == len(req.ApplyList) {
		return nil, gerror.Newf("apply friend failed, err: %v", errs[0])
	}
	rsp := &pb.RspFriendApply{}
	for _, apply := range newApply {
		rsp.ApplySendAddList = append(rsp.ApplySendAddList, &pb.PFriendApply{
			Base: r.packFriendBase(ctx, apply),
		})
	}

	return rsp, nil
}

func (r *RoleFriend) applyFriendSingle(ctx context.Context, friendID int64, source string) (*api.FriendInfo, error) {
	if friendID == r.RoleID {
		return nil, gerror.New("can not apply self")
	}
	if r.isFriend(friendID) {
		return nil, gerror.Newf("%d is friend already", friendID)
	}
	if r.isApplySend(friendID) {
		return nil, gerror.Newf("%d apply send already", friendID)
	}
	rsp, err := friend.FriendService().FriendApply(ctx, r.RoleID, friendID, source)
	if err != nil {
		return nil, gerror.Wrapf(err, "%d apply friend failed", friendID)
	}
	r.cache.applySendList = append(r.cache.applySendList, rsp.ApplyNew)
	return &rsp.ApplyNew, nil
}

func (r *RoleFriend) isFriend(friendID int64) bool {
	return r.isInFriendList(friendID, r.cache.friendList)
}

func (r *RoleFriend) isApplySend(friendID int64) bool {
	return r.isInFriendList(friendID, r.cache.applySendList)
}

func (r *RoleFriend) isApplyRecv(friendID int64) bool {
	return r.isInFriendList(friendID, r.cache.applyRecvList)
}

func (r *RoleFriend) isInFriendList(friendID int64, friendList []api.FriendInfo) bool {
	for _, friend := range friendList {
		if friend.FriendID == friendID {
			return true
		}
	}
	return false
}

func (r *RoleFriend) ReqFriendDealApply(ctx context.Context, req *pb.ReqFriendDealApply) (*pb.RspFriendDealApply, error) {
	if len(req.RoleId) == 0 {
		return nil, gerror.New("role id is empty")
	}
	var errs []error
	var applyRecvDeleted []*api.FriendInfo
	var friendNewList []*api.FriendInfo
	for _, friendID := range req.RoleId {
		if rsp, err := r.dealApplySingle(ctx, friendID, req.Deal); err != nil {
			errs = append(errs, err)
		} else {
			applyRecvDeleted = append(applyRecvDeleted, &rsp.ApplyDelete)
			if rsp.FriendNew.RoleID != 0 {
				friendNewList = append(friendNewList, &rsp.FriendNew)
			}
		}
	}
	// 一个都没有成功，返回错误
	if len(errs) == len(req.RoleId) {
		return nil, gerror.Newf("deal apply failed, err: %v", errs[0])
	}
	rsp := &pb.RspFriendDealApply{}
	for _, apply := range applyRecvDeleted {
		rsp.ApplyRecvDeleteList = append(rsp.ApplyRecvDeleteList, &pb.PFriendApply{
			Base: r.packFriendBase(ctx, apply),
		})
	}
	for _, friend := range friendNewList {
		rsp.FriendAddList = append(rsp.FriendAddList, r.packFriendInfo(ctx, friend))
	}
	return rsp, nil
}

func (r *RoleFriend) dealApplySingle(ctx context.Context, friendID int64, deal int32) (*api.DealApplyRes, error) {
	if friendID == r.RoleID {
		return nil, gerror.New("can not deal self")
	}
	if r.isFriend(friendID) {
		return nil, gerror.Newf("%d is friend already", friendID)
	}
	if !r.isApplyRecv(friendID) {
		return nil, gerror.Newf("%d apply recv not found", friendID)
	}
	rsp, err := friend.FriendService().DealApply(ctx, r.RoleID, friendID, deal)
	if err != nil {
		return nil, gerror.Wrapf(err, "%d deal apply failed", friendID)
	}
	r.cache.applyRecvList = r.removeFriendList(r.RoleID, friendID, r.cache.applyRecvList)
	friendID = rsp.FriendNew.FriendID
	if friendID != 0 {
		r.cache.friendList = append(r.cache.friendList, rsp.FriendNew)
		r.FriendDataList[friendID] = r.newFriendData(friendID)
	}
	return rsp, nil
}

func (r *RoleFriend) removeFriendList(roleID, friendID int64, list []api.FriendInfo) []api.FriendInfo {
	return slices.DeleteFunc(list, func(friend api.FriendInfo) bool {
		return friend.FriendID == friendID && friend.RoleID == roleID
	})
}

func (r *RoleFriend) newFriendData(friendID int64) FriendData {
	return FriendData{
		FriendID: friendID,
	}
}

func (r *RoleFriend) ReqFriendDelete(ctx context.Context, req *pb.ReqFriendDelete) (*pb.RspFriendDelete, error) {
	if len(req.RoleId) == 0 {
		return nil, gerror.New("role id is empty")
	}
	var errs []error
	var friendDeleted []int64
	for _, friendID := range req.RoleId {
		if frdID, err := r.deleteFriendSingle(ctx, friendID); err != nil {
			errs = append(errs, err)
		} else {
			friendDeleted = append(friendDeleted, frdID)
		}
	}
	// 一个都没有成功，返回错误
	if len(errs) == len(req.RoleId) {
		return nil, gerror.Newf("delete friend failed, err: %v", errs[0])
	}
	rsp := &pb.RspFriendDelete{
		FriendDelete: friendDeleted,
	}
	return rsp, nil
}

func (r *RoleFriend) deleteFriendSingle(ctx context.Context, friendID int64) (int64, error) {
	if !r.isFriend(friendID) {
		return 0, gerror.Newf("%d friend not found", friendID)
	}
	_, err := friend.FriendService().FriendDelete(ctx, r.RoleID, friendID)
	if err != nil {
		return 0, gerror.Wrapf(err, "%d delete friend failed", friendID)
	}
	r.cache.friendList = r.removeFriendList(r.RoleID, friendID, r.cache.friendList)
	delete(r.FriendDataList, friendID)
	return friendID, nil
}
