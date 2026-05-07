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
	{"order", "任务"},
	{"friend", "好友"},
	{"chat", "聊天"},
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

	// --- breed ---
	register(&Command{
		Name: "flower.info",
		Help: "Get flower info",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqFlowerInfo{})
		},
	})
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
			return c.Request(&pb.ReqStartBreed{FlowerId: int32(flowerID)})
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
			return c.Request(&pb.ReqFinishBreed{FlowerId: int32(flowerID)})
		},
	})

		// --- flower upgrade/break ---
		register(&Command{
			Name:   "flower.upgrade",
			Help:   "Upgrade a flower",
			Params: []string{"flower_id"},
			Exec: func(c *client.Client, args []string) error {
				if len(args) < 1 {
					return fmt.Errorf("usage: flower.upgrade <flower_id>")
				}
				flowerID, err := strconv.ParseInt(args[0], 10, 32)
				if err != nil {
					return fmt.Errorf("invalid flower_id: %v", err)
				}
				return c.Request(&pb.ReqUpgradeFlower{FlowerId: int32(flowerID)})
			},
		})
		register(&Command{
			Name:   "flower.break",
			Help:   "Break/ascend a flower",
			Params: []string{"flower_id"},
			Exec: func(c *client.Client, args []string) error {
				if len(args) < 1 {
					return fmt.Errorf("usage: flower.break <flower_id>")
				}
				flowerID, err := strconv.ParseInt(args[0], 10, 32)
				if err != nil {
					return fmt.Errorf("invalid flower_id: %v", err)
				}
				return c.Request(&pb.ReqBreakFlower{FlowerId: int32(flowerID)})
			},
		})

	// --- plot ---
	register(&Command{
		Name: "plot.info",
		Help: "Get plot info",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqPlotInfo{})
		},
	})
	register(&Command{
		Name:   "plot.unlock",
		Help:   "Unlock a plot",
		Params: []string{"plot_id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: plot.unlock <plot_id>")
			}
			plotID, err := strconv.ParseInt(args[0], 10, 32)
			if err != nil {
				return fmt.Errorf("invalid plot_id: %v", err)
			}
			return c.Request(&pb.ReqUnlockPlot{PlotId: int32(plotID)})
		},
	})
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
			return c.Request(&pb.ReqPlantFlower{
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
			return c.Request(&pb.ReqWaterFlower{PlotIds: plotIDs})
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
			return c.Request(&pb.ReqHarvestFlower{PlotIds: plotIDs})
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
			return c.Request(&pb.ReqRemovePlant{PlotIds: plotIDs})
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
			return c.Request(&pb.ReqFriendPlotInfo{FriendId: id})
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
			return c.Request(&pb.ReqStealFlower{FriendId: friendID, PlotId: int32(plotID)})
		},
	})

	// --- maintask ---
	register(&Command{
		Name: "maintask.info",
		Help: "Get main task info",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqMainTaskInfo{})
		},
	})
	register(&Command{
		Name: "maintask.claim",
		Help: "Claim main task reward",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqClaimMainTask{})
		},
	})

	// --- order ---
	register(&Command{
		Name: "order.info",
		Help: "Get order info",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqOrderInfo{})
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
			return c.Request(&pb.ReqSubmitOrder{SlotId: int32(slotID)})
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
			return c.Request(&pb.ReqClaimOrderMilestone{Id: int32(id)})
		},
	})

	// --- friend ---
	register(&Command{
		Name:   "friend.search",
		Help:   "Search players by name",
		Params: []string{"name"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.search <name>")
			}
			return c.Request(&pb.ReqSearchPlayer{Name: args[0]})
		},
	})
	register(&Command{
		Name:   "friend.send_request",
		Help:   "Send friend request to player(s)",
		Params: []string{"target_id..."},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.send_request <target_id...>")
			}
			ids := make([]int64, 0, len(args))
			for _, a := range args {
				id, err := strconv.ParseInt(a, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid target_id %q: %v", a, err)
				}
				ids = append(ids, id)
			}
			return c.Request(&pb.ReqSendRequest{TargetIds: ids})
		},
	})
	register(&Command{
		Name:   "friend.accept",
		Help:   "Accept friend request(s)",
		Params: []string{"from_id..."},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.accept <from_id...>")
			}
			ids := make([]int64, 0, len(args))
			for _, a := range args {
				id, err := strconv.ParseInt(a, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid from_id %q: %v", a, err)
				}
				ids = append(ids, id)
			}
			return c.Request(&pb.ReqAcceptRequest{FromIds: ids})
		},
	})
	register(&Command{
		Name:   "friend.reject",
		Help:   "Reject friend request(s)",
		Params: []string{"from_id..."},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.reject <from_id...>")
			}
			ids := make([]int64, 0, len(args))
			for _, a := range args {
				id, err := strconv.ParseInt(a, 10, 64)
				if err != nil {
					return fmt.Errorf("invalid from_id %q: %v", a, err)
				}
				ids = append(ids, id)
			}
			return c.Request(&pb.ReqRejectRequest{FromIds: ids})
		},
	})
	register(&Command{
		Name: "friend.list",
		Help: "Get friend list",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqFriendList{})
		},
	})
	register(&Command{
		Name: "friend.apply_list",
		Help: "Get friend apply list",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqApplyList{})
		},
	})
	register(&Command{
		Name:   "friend.remove",
		Help:   "Remove a friend",
		Params: []string{"target_id"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: friend.remove <target_id>")
			}
			id, err := strconv.ParseInt(args[0], 10, 64)
			if err != nil {
				return fmt.Errorf("invalid target_id: %v", err)
			}
			return c.Request(&pb.ReqRemoveFriend{TargetId: id})
		},
	})

	// --- chat ---
	register(&Command{
		Name: "chat.init",
		Help: "Initialize chat (login)",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqChatInit{})
		},
	})
	register(&Command{
		Name:   "chat.world",
		Help:   "Send message to world channel",
		Params: []string{"content"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: chat.world <content>")
			}
			return c.Request(&pb.ReqSendWorldChat{Content: args[0]})
		},
	})
	register(&Command{
		Name:   "chat.world_history",
		Help:   "Fetch world chat history",
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
			return c.Request(&pb.ReqWorldChatHistory{Count: count})
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
			return c.Request(&pb.ReqSendPrivateChat{TargetId: id, Content: args[1]})
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
			return c.Request(&pb.ReqPrivateChatHistory{FriendId: id, Count: count})
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
			return c.Request(&pb.ReqSystemChatHistory{Count: count})
		},
	})

	// --- gm ---
	register(&Command{
		Name:   "gm.cmd",
		Help:   "Execute GM command",
		Params: []string{"command"},
		Exec: func(c *client.Client, args []string) error {
			if len(args) < 1 {
				return fmt.Errorf("usage: gm.cmd <command>")
			}
			return c.Request(&pb.ReqGMCommand{Cmd: args[0]})
		},
	})
	register(&Command{
		Name: "gm.help",
		Help: "List available GM commands",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqGMHelp{})
		},
	})
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
