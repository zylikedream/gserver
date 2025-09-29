package role

import (
	"context"
	"fmt"
	"gserver/core/gxyactor"
	"gserver/core/gxymongo"
	"gserver/core/gxyregistery"
	"gserver/core/gxyservice"
	"gserver/protocol/pb"
	"gserver/service"
	"gserver/service/role/internal/logic"
	"gserver/util/uid"
	"time"

	"ergo.services/ergo/act"
	"ergo.services/ergo/gen"
	"github.com/gogf/gf/v2/os/glog"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
)

type roleServiceHelper struct {
}

type RoleAccount struct {
	Account string `bson:"account"`
	RoleID  int64  `bson:"role_id"`
}

var helper *roleServiceHelper

func NewRoleServiceHelper() *roleServiceHelper {
	helper = &roleServiceHelper{}
	return helper
}

func RoleServiceHelper() *roleServiceHelper {
	return helper
}
