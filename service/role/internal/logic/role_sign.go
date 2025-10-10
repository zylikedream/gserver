// -------------------------------------------------------------------
// @author zy<zylikedreams@github.com>
// @copyright 2022 zhangyi
// @doc
//
// @end
// @create: 2022-01-17
// -------------------------------------------------------------------
package logic

import (
	"context"
	"time"

	gameconfig "gserver/gameconfig/src"
	"gserver/protocol/pb"
	"gserver/util"

	"github.com/gogf/gf/v2/util/gconv"
	"github.com/gookit/goutil/arrutil"
	"github.com/pkg/errors"
)

// --------------------proto handlers start-------------
type RoleSign struct {
	RoleModule
	RoleID         int64     `bson:"role_id"`
	DrawTime       time.Time `bson:"sign_time"`        // 当日领取签到奖励
	SignDay        int       `bson:"sign_day"`         // 当月签到天数
	AccumDrawStage []int     `bson:"accum_draw_stage"` // 累积奖励阶段
	DrawDay        int       `bson:"reward_day"`       // 当月领取奖励的天数
	Patch          int       `bason:"patch"`           // 补签天数
}

func NewRoleSign() *RoleSign {
	return &RoleSign{}
}

func (r *RoleSign) OnInit(ctx context.Context) error {
	// r.GetRole().AddCron(ctx, gxyservice.DayRefresh, r.DayRefresh)
	return nil
}

// 刷新的时候自动签到(玩家上线，或者刷新点的时候在线，都算签到一次)
func (r *RoleSign) DayRefresh(ctx context.Context, tm time.Time) {
	r.SignDay += 1
}

func (s *RoleSign) ReqSignInfo(ctx context.Context, req *pb.ReqSignInfo) (*pb.RspSignInfo, error) {
	rsp := &pb.RspSignInfo{}
	rsp.MaxSignDay = int32(getMaxSignDay())
	rsp.AccumDrawStage = gconv.SliceInt32(s.AccumDrawStage)
	rsp.Patch = int32(s.Patch)
	rsp.SignDay = int32(s.SignDay)
	rsp.DrawDay = int32(s.DrawDay)

	return rsp, nil
}

// 领取每日奖励
func (s *RoleSign) ReqSignDraw(ctx context.Context, req *pb.ReqSignDraw) (*pb.RspSignDraw, error) {
	if util.IsSameDay(ctx, time.Now(), s.DrawTime) {
		return nil, errors.New("today is drawed")
	}
	rewards := s.getSignReward(s.DrawDay, s.SignDay)
	if err := s.GetRole().Bag.AddItemRc(ctx, rewards); err != nil {
		errors.Wrap(err, "add item failed")
	}
	s.DrawDay = s.SignDay
	s.DrawTime = time.Now()
	rsp := &pb.RspSignDraw{}
	rsp.SignTime = time.Now().Unix()
	return rsp, nil
}

func (s *RoleSign) getSignReward(from int, to int) []*gameconfig.ItemItemRC {
	vipLv := s.GetRole().Basic.VipLv
	rewards := []*gameconfig.ItemItemRC{}
	for i := from + 1; i <= to; i++ {
		signConfig := gameconfig.GameConfig().TbSignCheckIn.Get(int32(i))
		rewards = append(rewards, signConfig.Rewards...)
		if signConfig.Vip <= int32(vipLv) {
			// vip双倍
			rewards = append(rewards, signConfig.Rewards...)
		}
	}
	return rewards
}

// 补签
func (s *RoleSign) SignPatch(ctx context.Context, req *pb.ReqSignPatch) (*pb.RspSignPatch, error) {
	maxDay := getMaxSignDay()
	if req.PatchTimes <= 0 {
		return nil, errors.New("param unvalid")
	}
	if maxDay <= s.SignDay+int(req.PatchTimes) {
		return nil, errors.New("can't patch, sign full")
	}
	costs := []*gameconfig.ItemItemRC{}
	vipLv := s.GetRole().Basic.VipLv
	for i := 1; i <= int(req.PatchTimes); i++ {
		patchConfig := gameconfig.GameConfig().TbSignPatch.Get(int32(i + s.Patch))
		if vipLv < int(patchConfig.Vip) {
			return nil, errors.New("viplv not enough")
		}
		costs = append(costs, patchConfig.Costs...)
	}
	bag := s.GetRole().Bag
	if bag.CheckItemRc(ctx, costs) {
		return nil, errors.New("cost not enough")
	}
	// do
	if err := bag.DecItemRC(ctx, costs); err != nil {
		return nil, errors.Errorf("dec item failed: %v", costs)
	}
	// 补签奖励
	patchRewards := s.getSignReward(s.DrawDay+1, s.DrawDay+int(req.PatchTimes))
	if err := bag.AddItemRc(ctx, patchRewards); err != nil {
		return nil, errors.Errorf("add item failed: %v", patchRewards)
	}

	s.Patch += int(req.PatchTimes)
	s.SignDay += int(req.PatchTimes)
	s.DrawDay += int(req.PatchTimes)

	rsp := &pb.RspSignPatch{
		Patch:   int32(s.Patch),
		SignDay: int32(s.SignDay),
		DrawDay: int32(s.DrawDay),
	}
	return rsp, nil
}

// 累积签到奖励
func (r *RoleSign) ReqSignAccumDraw(ctx context.Context, req *pb.ReqSignAccumDraw) (*pb.RspSignAccumDraw, error) {
	if arrutil.IntsHas(r.AccumDrawStage, int(req.Stage)) {
		return nil, errors.New("stage getted")
	}
	accumConfig := gameconfig.GameConfig().TbSignAccum.Get(int32(req.Stage))
	if accumConfig == nil {
		return nil, errors.New("param unvalid")
	}
	if r.SignDay < int(accumConfig.NeedDays) {
		return nil, errors.New("accum day not enough")
	}
	if err := r.GetRole().Bag.AddItemRc(ctx, accumConfig.Rewards); err != nil {
		return nil, errors.New("add item failed")
	}
	r.AccumDrawStage = append(r.AccumDrawStage, int(req.Stage))
	rsp := &pb.RspSignAccumDraw{}
	for _, stage := range r.AccumDrawStage {
		rsp.AccumDrawStage = append(rsp.AccumDrawStage, int32(stage))
	}
	return rsp, nil
}

// --------------------proto handlers end-------------

func getMaxSignDay() int {
	return time.Now().Day()
}
