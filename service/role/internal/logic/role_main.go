package logic

import (
	"context"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxylocator"
	"gserver/core/gxymodule"
	"gserver/core/gxymongo"
	"gserver/util"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/glog"
	"github.com/pkg/errors"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

type RoleMainOption struct {
	Locator *gxylocator.Locator
}

type RoleMain struct {
	gxymodule.Module
	act.Actor
	RoleID int64

	Sign  *RoleSign
	Bag   *RoleBag
	Basic *RoleBasic

	timer    *gxyactor.ActorTimer
	modsHash map[string]uint64
	locator  *gxylocator.Locator
}

func NewRoleMain(opt RoleMainOption) *RoleMain {
	return &RoleMain{
		modsHash: map[string]uint64{},
		locator:  opt.Locator,
	}
}

func (r *RoleMain) Init(args ...any) error {
	RoleID, ok := args[0].(int64)
	if !ok {
		return fmt.Errorf("args[0] is not string")
	}
	r.RoleID = RoleID
	if err := r.registerRole(RoleID); err != nil {
		return err
	}
	ctx := context.Background()
	r.Sign = NewRoleSign()
	r.AddModule(ctx, r.Sign)

	r.Bag = NewRoleBag()
	r.AddModule(ctx, r.Bag)

	r.Basic = NewRoleBasic()
	r.AddModule(ctx, r.Basic)

	r.Send(r.PID(), "init_role")
	return nil
}

func (r *RoleMain) HandleMessage(from gen.PID, msg any) error {
	switch m := msg.(type) {
	case string:
		if m != "init_role" {
			err := r.initRole()
			if err != nil {
				return err
			}
		}
	case *gxyactor.TimerMsg:
		m.Fun(context.Background(), m.Time)
	}
	return nil
}

func (r *RoleMain) initRole() error {
	if err := r.initRoleModules(); err != nil {
		return err
	}
	r.timer = gxyactor.NewActorTimer(r.PID())
	r.timer.AddTick(context.Background(), &gxyactor.Tick{
		Tick: 1 * time.Second,
	}, func(ctx context.Context, tm time.Time) {
		r.save(ctx, false)
	})
	return nil
}

func (r *RoleMain) initRoleModules() error {
	roleID := r.RoleID
	ctx := context.Background()
	for _, mod := range r.Modules() {
		if err := gxymongo.Client().FindOne(ctx, roleID, mod.GetName(), mod); err != nil {
			if err != mongo.ErrNoDocuments {
				return err
			}
		}
		r.modsHash[mod.GetName()] = util.GetObjectHash(mod)
	}
	for _, mod := range r.Modules() {
		rmod := mod.(IRoleModule)
		if err := rmod.AfterInit(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (r *RoleMain) save(ctx context.Context, force bool) error {
	var errStr string
	client := gxymongo.Client()
	for _, mod := range r.Modules() {
		modName := mod.GetName()
		modHash := util.GetObjectHash(mod)
		if modHash == r.modsHash[modName] && !force {
			continue
		}
		if _, err := client.ReplaceOne(ctx, modName, bson.M{"_id": r.RoleID},
			mod, options.Replace().SetUpsert(true)); err != nil {
			errStr += fmt.Sprintf("save mod %s failed: %s", modName, err)
			continue
		}
		r.modsHash[modName] = modHash
	}
	if errStr != "" {
		return errors.New(errStr)
	}
	return nil
}

func (r *RoleMain) Terminate(reason error) {
	if err := r.unRegisterRole(r.RoleID); err != nil {
		glog.Errorf(context.Background(), "unregister role failed: %s", err)
	}
	if err := r.save(context.Background(), true); err != nil {
		glog.Errorf(context.Background(), "save role failed: %s", err)
	}
}

func (r *RoleMain) registerRole(roleID int64) error {
	return r.locator.Register(context.Background(), fmt.Sprintf("%d", roleID),
		gxyactor.ActorSystem().GetNodeName())
}

func (r *RoleMain) unRegisterRole(roleID int64) error {
	return r.locator.Unregister(context.Background(), fmt.Sprintf("%d", roleID))
}
