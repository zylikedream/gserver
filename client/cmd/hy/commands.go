package main

import (
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
	Group  string
	Exec   func(c *client.Client, args []string) error
}

var commands = map[string]*Command{}
var commandOrder []string

func register(cmd *Command) {
	commands[cmd.Name] = cmd
	commandOrder = append(commandOrder, cmd.Name)
}

var groupDefs = []struct {
	prefix, label string
}{
	{"basic", "角色"},
	{"bag", "背包"},
	{"flower", "花园"},
	{"breed", "花园"},
	{"plot", "花园"},
	{"maintask", "任务"},
	{"residentorder", "任务"},
	{"order", "任务"},
	{"friend", "好友"},
	{"chat", "聊天"},
	{"guild", "公会"},
	{"gm", "GM"},
}

func groupOf(name string) string {
	prefix := strings.SplitN(name, ".", 2)[0]
	for _, g := range groupDefs {
		if prefix == g.prefix {
			return g.label
		}
	}
	return "其他"
}

func init() {
	// --- breed (aliases for flower.start_breed / flower.finish_breed) ---
	register(&Command{
		Name:   "breed.start",
		Help:   "Start breeding a flower",
		Params: []string{"flower_id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: breed.start <flower_id>")
			}
			flowerID, err := strconv.ParseInt(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid flower_id: %v", err)
			}
			return c.Request(&pb.ReqFlowerStartBreed{FlowerId: int32(flowerID)})
		},
	})
	register(&Command{
		Name:   "breed.finish",
		Help:   "Finish breeding a flower",
		Params: []string{"flower_id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: breed.finish <flower_id>")
			}
			flowerID, err := strconv.ParseInt(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid flower_id: %v", err)
			}
			return c.Request(&pb.ReqFlowerFinishBreed{FlowerId: int32(flowerID)})
		},
	})

	// --- flower (plot commands with flower. prefix + custom parsing) ---
	register(&Command{
		Name:   "flower.plant",
		Help:   "Plant a flower in plots",
		Params: []string{"flower_id", "plot_ids..."},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: flower.plant <flower_id> <plot_id...>")
			}
			flowerID, err := strconv.ParseInt(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid flower_id: %v", err)
			}
			plotIDs := parsePlotIDs(args[1:])
			return c.Request(&pb.ReqPlotPlant{
				FlowerId: int32(flowerID),
				PlotIds:  plotIDs,
			})
		},
	})
	register(&Command{
		Name:   "flower.water",
		Help:   "Water flowers in plots",
		Params: []string{"plot_ids..."},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: flower.water <plot_id...>")
			}
			plotIDs := parsePlotIDs(args)
			return c.Request(&pb.ReqPlotWater{PlotIds: plotIDs})
		},
	})
	register(&Command{
		Name:   "flower.harvest",
		Help:   "Harvest flowers from plots",
		Params: []string{"plot_ids..."},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: flower.harvest <plot_id...>")
			}
			plotIDs := parsePlotIDs(args)
			return c.Request(&pb.ReqPlotHarvest{PlotIds: plotIDs})
		},
	})
	register(&Command{
		Name:   "flower.remove",
		Help:   "Remove plants from plots",
		Params: []string{"plot_ids..."},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: flower.remove <plot_id...>")
			}
			plotIDs := parsePlotIDs(args)
			return c.Request(&pb.ReqPlotRemove{PlotIds: plotIDs})
		},
	})
	register(&Command{
		Name:   "flower.friend_plot",
		Help:   "View friend's garden plots",
		Params: []string{"friend_id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: flower.friend_plot <friend_id>")
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid friend_id: %v", err)
			}
			return c.Request(&pb.ReqPlotFriendInfo{FriendId: id})
		},
	})
	register(&Command{
		Name:   "flower.steal",
		Help:   "Steal a flower from friend's garden",
		Params: []string{"friend_id", "plot_id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: flower.steal <friend_id> <plot_id>")
			}
			friendID, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid friend_id: %v", err)
			}
			plotID, err := strconv.ParseInt(args[1], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid plot_id: %v", err)
			}
			return c.Request(&pb.ReqPlotSteal{FriendId: friendID, PlotId: int32(plotID)})
		},
	})

	// --- chat (custom channel type parsing) ---
	register(&Command{
		Name:   "chat.send",
		Help:   "Send message to a channel (world/guild)",
		Params: []string{"channel_type", "content"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: chat.send <world|guild> <content>")
			}
			ct, err := parseChannelType(args[0])
			if err != nil {
				return err
			}
			return c.Request(&pb.ReqChatSendChannel{ChannelType: ct, Content: args[1]})
		},
	})
	register(&Command{
		Name:   "chat.history",
		Help:   "Fetch channel chat history",
		Params: []string{"channel_type", "channel_id", "count"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: chat.history <world|guild> [channel_id] [count]")
			}
			ct, err := parseChannelType(args[0])
			if err != nil {
				return err
			}
			chID := int64(0)
			if len(args) >= 2 {
				chID, err = strconv.ParseInt(args[1], 10, 64)
				if err != nil {
					return fmt.Errorf("invalid channel_id: %v", err)
				}
			}
			count := int32(20)
			if len(args) >= 3 {
				n, err := strconv.ParseInt(args[2], 10, 32)
				if err != nil {
					return fmt.Errorf("invalid count: %v", err)
				}
				count = int32(n)
			}
			return c.Request(&pb.ReqChatChannelHistory{ChannelType: ct, ChannelId: chID, Count: count})
		},
	})
	register(&Command{
		Name:   "chat.private",
		Help:   "Send private message to a friend",
		Params: []string{"target_id", "content"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: chat.private <target_id> <content>")
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid target_id: %v", err)
			}
			return c.Request(&pb.ReqChatSendPrivate{TargetId: id, Content: args[1]})
		},
	})
	register(&Command{
		Name:   "chat.private_history",
		Help:   "Fetch private chat history with a friend",
		Params: []string{"friend_id", "count"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: chat.private_history <friend_id> [count]")
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid friend_id: %v", err)
			}
			count := int32(20)
			if len(args) >= 2 {
				n, err := strconv.ParseInt(args[1], 10, 32)
				if err != nil {
					return fmt.Errorf("invalid count: %v", err)
				}
				count = int32(n)
			}
			return c.Request(&pb.ReqChatPrivateHistory{FriendId: id, Count: count})
		},
	})
	register(&Command{
		Name:   "chat.system_history",
		Help:   "Fetch system chat history",
		Params: []string{"count"},
		Exec: func(c *client.Client, args []string) error {
			count := int32(20)
			if len(args) >= 1 {
				n, err := strconv.ParseInt(args[0], 10, 32)
				if err != nil {
					return fmt.Errorf("invalid count: %v", err)
				}
				count = int32(n)
			}
			return c.Request(&pb.ReqChatSystemHistory{Count: count})
		},
	})

	// --- guild (commands with custom parsing) ---
	register(&Command{
		Name:   "guild.approve",
		Help:   "Approve/reject guild applications",
		Params: []string{"apply_ids...", "approve"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 2 {
				return fmt.Errorf("usage: guild.approve <apply_id...> 1|0")
			}
			approve := args[len(args)-1] == "1"
			ids := make([]int64, 0, len(args)-1)
			for _, a := range args[:len(args)-1] {
				id, err := strconv.ParseInt(a, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid apply_id %q: %v", a, err)
				}
				ids = append(ids, id)
			}
			return c.Request(&pb.ReqGuildApproveApply{ApplyIds: ids, Approve: approve})
		},
	})
	register(&Command{
		Name:   "guild.kick",
		Help:   "Kick a member from guild",
		Params: []string{"target_id", "reason"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: guild.kick <target_id> [reason]")
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid target_id: %v", err)
			}
			reason := ""
			if len(args) >= 2 {
				reason = args[1]
			}
			return c.Request(&pb.ReqGuildKickMember{TargetId: id, Reason: reason})
		},
	})
	register(&Command{
		Name:   "guild.update_info",
		Help:   "Update guild info",
		Params: []string{"declaration", "announcement", "need_approval"},
		Exec: func(c *client.Client, args []string) error {
			declaration := ""
			announcement := ""
			needApproval := false
			if len(args) >= 1 {
				declaration = args[0]
			}
			if len(args) >= 2 {
				announcement = args[1]
			}
			if len(args) >= 3 {
				needApproval = args[2] == "1"
			}
			return c.Request(&pb.ReqGuildUpdateInfo{
				Declaration:  declaration,
				Announcement: announcement,
				NeedApproval: needApproval,
			})
		},
	})

	// --- order (shorter aliases for residentorder commands) ---
	register(&Command{
		Name: "order.info",
		Help: "Get order info",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqResidentOrderInfo{})
		},
	})
	register(&Command{
		Name:   "order.submit",
		Help:   "Submit an order",
		Params: []string{"slot_id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: order.submit <slot_id>")
			}
			slotID, err := strconv.ParseInt(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid slot_id: %v", err)
			}
			return c.Request(&pb.ReqResidentOrderSubmit{SlotId: int32(slotID)})
		},
	})
	register(&Command{
		Name:   "order.claim",
		Help:   "Claim order milestone reward",
		Params: []string{"id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: order.claim <id>")
			}
			id, err := strconv.ParseInt(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid id: %v", err)
			}
			return c.Request(&pb.ReqResidentOrderClaimMilestone{Id: int32(id)})
		},
	})

	// Auto-register remaining commands from proto definitions
	registerAutoCommands()
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

func prettyPrintResponse(msg proto.Message) bool {
	switch m := msg.(type) {
	case *pb.RspGMHelp:
		printGMHelp(m)
		return true
	case *pb.RspGMCommand:
		printGMResult(m)
		return true
	}
	return false
}

func printGMHelp(rsp *pb.RspGMHelp) {
	fmt.Println("← RspGMHelp")
	if len(rsp.Commands) == 0 {
		fmt.Println("  (no commands)")
		return
	}
	for _, cmd := range rsp.Commands {
		fmt.Printf("  %-25s %s\n", cmd.Name, cmd.Brief)
		if cmd.Usage != "" {
			fmt.Printf("  %27s%s\n", "", cmd.Usage)
		}
		if cmd.Example != "" {
			fmt.Printf("  %27sex: %s\n", "", cmd.Example)
		}
	}
}

func printGMResult(rsp *pb.RspGMCommand) {
	fmt.Println("← RspGMCommand")
	if rsp.Result != "" {
		for _, line := range strings.Split(rsp.Result, "\\n") {
			fmt.Printf("  %s\n", line)
		}
	}
}

func parsePlotIDs(args []string) []int32 {
	var ids []int32
	for _, a := range args {
		for _, p := range strings.Split(a, ",") {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			id, err := strconv.ParseInt(p, 10, 32)
			if err != nil {
				continue
			}
			ids = append(ids, int32(id))
		}
	}
	return ids
}

func parseChannelType(s string) (int32, error) {
	switch strings.ToLower(s) {
	case "world", "1":
		return 1, nil
	case "guild", "4":
		return 4, nil
	default:
		n, err := strconv.ParseInt(s, 10, 32)
		if err != nil {
			return 0, fmt.Errorf("unknown channel: %s (use world=1 / guild=4 / system=3)", s)
		}
		return int32(n), nil
	}
}
