package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"strings"

	"hy_client/pkg/client"

	"github.com/BurntSushi/toml"
	"google.golang.org/protobuf/proto"
)

type fileConfig struct {
	Server struct {
		Addr    string `toml:"addr"`
		Account string `toml:"account"`
	} `toml:"server"`
}

func main() {
	addr := flag.String("addr", "", "server address (host:port)")
	account := flag.String("account", "", "account uid")
	configFile := flag.String("config", "", "config file path")
	flag.Parse()

	cfg := &fileConfig{}
	if *configFile != "" {
		if _, err := toml.DecodeFile(*configFile, cfg); err != nil {
			fmt.Printf("failed to load config: %v\n", err)
			os.Exit(1)
		}
	}

	serverAddr := cfg.Server.Addr
	if *addr != "" {
		serverAddr = *addr
	}
	defaultAccount := cfg.Server.Account
	if *account != "" {
		defaultAccount = *account
	}

	if serverAddr == "" {
		fmt.Println("error: server address required (--addr or config file)")
		os.Exit(1)
	}

	client.RegisterMessages()

	accountUID := promptAccount(defaultAccount)

	c := client.NewClient(client.Config{
		Addr:       serverAddr,
		AccountUID: accountUID,
	})

	c.OnMessage(func(msg proto.Message) {
		printProtoJSON("[push]", msg)
	})

	c.OnResponse(func(msg proto.Message) {
		printProtoJSON("←", msg)
	})

	fmt.Printf("connecting to %s...\n", serverAddr)
	if err := c.Connect(); err != nil {
		fmt.Printf("connect failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("connected.")

	rsp, err := c.Handshake()
	if err != nil {
		fmt.Printf("handshake failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("handshake ok, role_id=%d\n", rsp.RoleId)

	loginRsp, err := c.Login()
	if err != nil {
		fmt.Printf("login failed: %v\n", err)
		os.Exit(1)
	}
	if loginRsp.FirstLogin {
		fmt.Println("first login!")
	}
	fmt.Println("login ok.")

	repl := NewREPL(c)
	repl.Run()

	c.Close()
}

func promptAccount(defaultAccount string) string {
	if defaultAccount != "" {
		fmt.Printf("Account [%s]: ", defaultAccount)
	} else {
		fmt.Print("Account: ")
	}

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		if defaultAccount == "" {
			fmt.Println("error: account required")
			os.Exit(1)
		}
		return defaultAccount
	}
	return input
}
