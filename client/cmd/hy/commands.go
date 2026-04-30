package main

import (
	"fmt"
	"strconv"

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

	// --- breed ---
	register(&Command{
		Name: "breed.info",
		Help: "Get breed (flower) info",
		Exec: func(c *client.Client, args []string) error {
			return c.Request(&pb.ReqBreedInfo{})
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
