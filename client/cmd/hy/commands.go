package main

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"hy_client/pb"
	"hy_client/pkg/client"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/proto"
)

type Command struct {
	Name   string
	Help   string
	Params []string
	Exec   func(c *client.Client, args []string) error
}

var commands = map[string]*Command{}

func register(cmd *Command) {
	commands[cmd.Name] = cmd
}

func init() {
	// --- basic ---
	register(&Command{
		Name: "basic.info",
		Help: "Get basic role info",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqBasicInfo{})
		},
	})
	register(&Command{
		Name:   "basic.set_name",
		Help:   "Set role name",
		Params: []string{"name"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: basic.set_name <name>")
			}
			return c.Request(&pb.ReqBasicSetName{Name: args[0]})
		},
	})
	register(&Command{
		Name:   "basic.set_head",
		Help:   "Set role head icon",
		Params: []string{"head"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: basic.set_head <head>")
			}
			return c.Request(&pb.ReqBasicSetHead{Head: args[0]})
		},
	})

	// --- bag ---
	register(&Command{
		Name: "bag.info",
		Help: "Get bag info",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqBagInfo{})
		},
	})

	// --- sign ---
	register(&Command{
		Name: "sign.info",
		Help: "Get sign-in info",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqSignInfo{})
		},
	})
	register(&Command{
		Name: "sign.draw",
		Help: "Draw daily sign-in reward",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqSignDraw{})
		},
	})
	register(&Command{
		Name:   "sign.patch",
		Help:   "Patch missed sign-in days",
		Params: []string{"times"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: sign.patch <times>")
			}
			times, err := strconv.ParseUint(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid times: %v", err)
			}
			return c.Request(&pb.ReqSignPatch{PatchTimes: uint32(times)})
		},
	})
	register(&Command{
		Name:   "sign.accum_draw",
		Help:   "Draw accumulated sign-in reward",
		Params: []string{"stage"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: sign.accum_draw <stage>")
			}
			stage, err := strconv.ParseInt(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid stage: %v", err)
			}
			return c.Request(&pb.ReqSignAccumDraw{Stage: int32(stage)})
		},
	})

	// --- friend ---
	register(&Command{
		Name: "friend.info",
		Help: "Get friend list info",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqFriendInfo{})
		},
	})
	register(&Command{
		Name:   "friend.apply",
		Help:   "Apply to add friends",
		Params: []string{"[{\"role_id\":X,\"source\":\"Y\"},...]"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.apply [{\"role_id\":1001,\"source\":\"search\"}]")
			}
			raw := strings.Join(args, " ")
			var applyList []*pb.PFriendApplyData
			if err := json.Unmarshal([]byte(raw), &applyList); err != nil {
				return fmt.Errorf("invalid JSON array: %v", err)
			}
			return c.Request(&pb.ReqFriendApply{ApplyList: applyList})
		},
	})
	register(&Command{
		Name:   "friend.deal_apply",
		Help:   "Accept(1) or reject(0) friend applications",
		Params: []string{"[role_id,...]", "deal(0|1)"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: friend.deal_apply [1001,1002] 1")
			}
			roleIDs, err := parseArrayInt64(args[0])
			if err != nil {
				return fmt.Errorf("invalid role_ids: %v", err)
			}
			deal, err := strconv.ParseInt(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid deal: %v", err)
			}
			return c.Request(&pb.ReqFriendDealApply{RoleId: roleIDs, Deal: int32(deal)})
		},
	})
	register(&Command{
		Name:   "friend.delete",
		Help:   "Delete friends",
		Params: []string{"[role_id,...]"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.delete [1001,1002]")
			}
			roleIDs, err := parseArrayInt64(args[0])
			if err != nil {
				return fmt.Errorf("invalid role_ids: %v", err)
			}
			return c.Request(&pb.ReqFriendDelete{RoleId: roleIDs})
		},
	})
	register(&Command{
		Name:   "friend.send_gift",
		Help:   "Send gift to friend",
		Params: []string{"role_id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.send_gift <role_id>")
			}
			roleID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid role_id: %v", err)
			}
			return c.Request(&pb.ReqFriendSendGift{RoleId: roleID})
		},
	})
	register(&Command{
		Name:   "friend.recv_gift",
		Help:   "Receive gift from friend",
		Params: []string{"role_id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.recv_gift <role_id>")
			}
			roleID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid role_id: %v", err)
			}
			return c.Request(&pb.ReqFriendRecvGift{RoleId: roleID})
		},
	})

	// --- mahong ---
	register(&Command{
		Name:   "mahong.create_room",
		Help:   "Create mahjong room",
		Params: []string{"game_mode", "play_turn", "max_fan", "max_player"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 4 {
				return fmt.Errorf("usage: mahong.create_room <game_mode> <play_turn> <max_fan> <max_player>")
			}
			mode, _ := strconv.ParseInt(args[0], 10, 32)
			turn, _ := strconv.ParseInt(args[1], 10, 32)
			fan, _ := strconv.ParseInt(args[2], 10, 32)
			player, _ := strconv.ParseInt(args[3], 10, 32)
			return c.Request(&pb.ReqMahongCreateRoom{
				Rule: &pb.PMahongRuleInfo{
					GameMode:  int32(mode),
					PlayTurn:  int32(turn),
					MaxFan:    int32(fan),
					MaxPlayer: int32(player),
				},
			})
		},
	})
	register(&Command{
		Name:   "mahong.join_room",
		Help:   "Join mahjong room",
		Params: []string{"room_id", "identify"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: mahong.join_room <room_id> <identify>")
			}
			roomID, _ := strconv.ParseInt(args[0], 10, 64)
			identify, _ := strconv.ParseInt(args[1], 10, 32)
			return c.Request(&pb.ReqMahongJoinRoom{RoomId: roomID, Identify: int32(identify)})
		},
	})
	register(&Command{
		Name:   "mahong.operate",
		Help:   "Send mahjong operate command",
		Params: []string{"cmd", "val"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: mahong.operate <cmd> <val>")
			}
			cmd, _ := strconv.ParseInt(args[0], 10, 32)
			val, _ := strconv.ParseInt(args[1], 10, 32)
			return c.Request(&pb.ReqMahongOperate{Cmd: int32(cmd), Val: int32(val)})
		},
	})
	register(&Command{
		Name:   "mahong.set_ready",
		Help:   "Set ready state",
		Params: []string{"ready(0|1)"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: mahong.set_ready <ready>")
			}
			ready, _ := strconv.ParseInt(args[0], 10, 32)
			return c.Request(&pb.ReqMahongSetReady{Ready: int32(ready)})
		},
	})
}

func parseArrayInt64(s string) ([]int64, error) {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "[") || !strings.HasSuffix(s, "]") {
		return nil, fmt.Errorf("array must be wrapped in [], got: %s", s)
	}
	s = s[1 : len(s)-1]
	if s == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	result := make([]int64, 0, len(parts))
	for _, p := range parts {
		v, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid number: %s", p)
		}
		result = append(result, v)
	}
	return result, nil
}

func printProtoJSON(prefix string, msg proto.Message) {
	name := string(proto.MessageName(msg))
	marshaler := protojson.MarshalOptions{EmitDefaultValues: true}
	jsonBytes, err := marshaler.Marshal(msg)
	if err != nil {
		fmt.Printf("%s %s <marshal error: %v>\n", prefix, name, err)
		return
	}
	fmt.Printf("%s %s %s\n", prefix, name, string(jsonBytes))
}
