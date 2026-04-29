package logic

import (
	"context"
	"strings"

	"gserver/protocol/pb"
	"gserver/src/apps/role/internal/gm"
)

type RoleGM struct {
	RoleModule
}

var _ IRoleModule = (*RoleGM)(nil)

func (r *RoleGM) ReqGMCommand(ctx context.Context, req *pb.ReqGMCommand) (*pb.RspGMCommand, error) {
	parts := strings.Fields(req.Cmd)
	if len(parts) == 0 {
		return &pb.RspGMCommand{Result: "empty command"}, nil
	}
	g := gm.NewGM(ctx, r.Role.Bag)
	err := g.ExecCommand(parts[0], parts[1:])
	if err != nil {
		return &pb.RspGMCommand{Result: err.Error()}, nil
	}
	return &pb.RspGMCommand{Result: "ok"}, nil
}

func (r *RoleGM) ReqGMHelp(ctx context.Context, req *pb.ReqGMHelp) (*pb.RspGMHelp, error) {
	docs := gm.GetCmdDocs()
	cmds := make([]*pb.PCmdDoc, 0, len(docs))
	for _, d := range docs {
		cmds = append(cmds, &pb.PCmdDoc{
			Name:    d.Name,
			Brief:   d.Brief,
			Usage:   d.Usage,
			Example: d.Example,
		})
	}
	return &pb.RspGMHelp{Commands: cmds}, nil
}
