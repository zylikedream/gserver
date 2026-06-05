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
	AccountServer struct {
		URL          string `toml:"url"`
		Platform     string `toml:"platform"`
		PlatformUID  string `toml:"platform_uid"`
		ClientVersion string `toml:"client_version"`
	} `toml:"account_server"`
}

func main() {
	addr := flag.String("addr", "", "gate server address (host:port)")
	accountServer := flag.String("account-server", "", "account server URL (e.g. http://account.example.com)")
	platform := flag.String("platform", "guest", "platform identifier (guest/wechat/apple)")
	platformUID := flag.String("platform-uid", "", "platform user ID")
	clientVersion := flag.String("client-version", "", "client version")
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

	acctServer := cfg.AccountServer.URL
	if *accountServer != "" {
		acctServer = *accountServer
	}
	plat := cfg.AccountServer.Platform
	if *platform != "guest" || plat == "" {
		plat = *platform
	}
	pUID := cfg.AccountServer.PlatformUID
	if *platformUID != "" {
		pUID = *platformUID
	}
	cliVer := cfg.AccountServer.ClientVersion
	if *clientVersion != "" {
		cliVer = *clientVersion
	}

	client.RegisterMessages()

	var gateAddr string
	var gateToken string

	if acctServer != "" {
		pUID = promptPlatformUID(pUID)
		fmt.Printf("prelogin to %s ...\n", acctServer)
		preloginData, err := client.AccountServerPrelogin(acctServer, plat, pUID, cliVer)
		if err != nil {
			fmt.Printf("prelogin failed: %v\n", err)
			os.Exit(1)
		}
		if preloginData.IsNewRole {
			fmt.Println("new account created!")
		}
		gateAddr = fmt.Sprintf("%s:%d", preloginData.Gate.Host, preloginData.Gate.Port)
		gateToken = preloginData.GateToken
		fmt.Printf("prelogin ok, role_id=%d, gate=%s\n", preloginData.RoleID, gateAddr)
	} else {
		gateAddr = serverAddr
		if gateAddr == "" {
			fmt.Println("error: server address required (--addr or config file), or use --account-server for prelogin")
			os.Exit(1)
		}
	}

	c := client.NewClient(client.Config{
		Addr: gateAddr,
	})

	c.OnMessage(func(msg proto.Message) {
		fmt.Println()
		printProtoJSON("[push]", msg)
	})

	c.OnResponse(func(msg proto.Message) {
		if prettyPrintResponse(msg) {
			return
		}
		printProtoJSON("←", msg)
	})

	c.OnDisconnect(func(reason error) {
		fmt.Printf("\n⚠ disconnected: %v\n", reason)
	})

	fmt.Printf("connecting to %s...\n", gateAddr)
	if err := c.Connect(); err != nil {
		fmt.Printf("connect failed: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("connected.")

	if gateToken != "" {
		rsp, err := c.Handshake(gateToken)
		if err != nil {
			fmt.Printf("handshake failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("handshake ok, role_id=%d\n", rsp.RoleId)
	}

	loginRsp, err := c.Login()
	if err != nil {
		fmt.Printf("login failed: %v\n", err)
		os.Exit(1)
	}
	if loginRsp.FirstLogin {
		fmt.Println("first login!")
	}
	fmt.Println("login ok.")

	repl := newREPL(c, acctServer, plat, pUID, cliVer)
	repl.Run()

	c.Close()
}

func promptPlatformUID(defaultUID string) string {
	if defaultUID != "" {
		fmt.Printf("Platform UID [%s]: ", defaultUID)
	} else {
		fmt.Print("Platform UID: ")
	}

	reader := bufio.NewReader(os.Stdin)
	input, _ := reader.ReadString('\n')
	input = strings.TrimSpace(input)

	if input == "" {
		if defaultUID == "" {
			fmt.Println("error: platform_uid required")
			os.Exit(1)
		}
		return defaultUID
	}
	return input
}
