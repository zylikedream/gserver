package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"gserver/client/pkg/client"

	"github.com/BurntSushi/toml"
	"google.golang.org/protobuf/proto"
)

type fileConfig struct {
	Server struct {
		Addr    string `toml:"addr"`
		Account string `toml:"account"`
	} `toml:"server"`
	AccountServer struct {
		URL           string `toml:"url"`
		Platform      string `toml:"platform"`
		PlatformUID   string `toml:"platform_uid"`
		ClientVersion string `toml:"client_version"`
	} `toml:"account_server"`
}

// cliOptions 承载命令行参数。
type cliOptions struct {
	addr          string
	accountServer string
	platform      string
	platformUID   string
	clientVersion string
	configFile    string
}

func parseCLI() cliOptions {
	o := cliOptions{}
	flag.StringVar(&o.addr, "addr", "", "gate server address (host:port)")
	flag.StringVar(&o.accountServer, "account-server", "", "account server URL (e.g. http://account.example.com)")
	flag.StringVar(&o.platform, "platform", "guest", "platform identifier (guest/wechat/apple)")
	flag.StringVar(&o.platformUID, "platform-uid", "", "platform user ID")
	flag.StringVar(&o.clientVersion, "client-version", "", "client version")
	flag.StringVar(&o.configFile, "config", "", "config file path")
	flag.Parse()
	return o
}

// loadFileConfig 读取配置文件;未指定返回空配置,读取失败即退出。
func loadFileConfig(path string) *fileConfig {
	cfg := &fileConfig{}
	if path == "" {
		return cfg
	}
	if _, err := toml.DecodeFile(path, cfg); err != nil {
		fmt.Printf("failed to load config: %v\n", err)
		os.Exit(1)
	}
	return cfg
}

// accountConfig 是解析后的账号/接入参数(命令行优先于配置文件)。
type accountConfig struct {
	acctServer    string
	platform      string
	platformUID   string
	clientVersion string
}

func resolveAccount(cfg *fileConfig, o cliOptions) accountConfig {
	ac := accountConfig{
		acctServer:    cfg.AccountServer.URL,
		platform:      cfg.AccountServer.Platform,
		platformUID:   cfg.AccountServer.PlatformUID,
		clientVersion: cfg.AccountServer.ClientVersion,
	}
	if o.accountServer != "" {
		ac.acctServer = o.accountServer
	}
	if o.platform != "guest" || ac.platform == "" {
		ac.platform = o.platform
	}
	if o.platformUID != "" {
		ac.platformUID = o.platformUID
	}
	if o.clientVersion != "" {
		ac.clientVersion = o.clientVersion
	}
	return ac
}

func main() {
	o := parseCLI()
	cfg := loadFileConfig(o.configFile)
	ac := resolveAccount(cfg, o)

	client.RegisterMessages()

	if ac.acctServer != "" {
		ac.platformUID = promptPlatformUID(ac.platformUID)
	}
	gateAddr, gateToken, err := resolveGate(ac, o.addr, cfg.Server.Addr)
	if err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	c := client.NewClient(client.Config{Addr: gateAddr})
	setupHandlers(c)
	if err := connectAndAuth(c, gateAddr, gateToken); err != nil {
		fmt.Printf("%v\n", err)
		os.Exit(1)
	}

	newREPL(c, ac.acctServer, ac.platform, ac.platformUID, ac.clientVersion).Run()
	_ = c.Close()
}

// resolveGate 决定接入点:配了 account server 则预登录换 gate 地址与 token,否则用直连地址。
func resolveGate(ac accountConfig, addrFlag, cfgAddr string) (gateAddr, gateToken string, err error) {
	if ac.acctServer == "" {
		addr := cfgAddr
		if addrFlag != "" {
			addr = addrFlag
		}
		if addr == "" {
			return "", "", fmt.Errorf("server address required (--addr or config file), or use --account-server for prelogin")
		}
		return addr, "", nil
	}

	fmt.Printf("prelogin to %s ...\n", ac.acctServer)
	data, err := client.AccountServerPrelogin(ac.acctServer, ac.platform, ac.platformUID, ac.clientVersion)
	if err != nil {
		return "", "", fmt.Errorf("prelogin failed: %w", err)
	}
	if data.IsNewRole {
		fmt.Println("new account created!")
	}
	gateAddr = fmt.Sprintf("%s:%d", data.Gate.Host, data.Gate.Port)
	fmt.Printf("prelogin ok, role_id=%d, gate=%s\n", data.RoleID, gateAddr)
	return gateAddr, data.GateToken, nil
}

// setupHandlers 注册客户端的推送/响应/断开回调。
func setupHandlers(c *client.Client) {
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
}

// connectAndAuth 连接 gate,并在有 token 时握手,然后登录。
func connectAndAuth(c *client.Client, gateAddr, gateToken string) error {
	fmt.Printf("connecting to %s...\n", gateAddr)
	if err := c.Connect(); err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}
	fmt.Println("connected.")

	if gateToken != "" {
		rsp, err := c.Handshake(gateToken)
		if err != nil {
			return fmt.Errorf("handshake failed: %w", err)
		}
		fmt.Printf("handshake ok, role_id=%d\n", rsp.RoleId)
	}

	loginRsp, err := c.Login()
	if err != nil {
		return fmt.Errorf("login failed: %w", err)
	}
	if loginRsp.FirstLogin {
		fmt.Println("first login!")
	}
	fmt.Println("login ok.")
	return nil
}

func promptPlatformUID(defaultUID string) string {
	if defaultUID != "" {
		fmt.Printf("Platform UID [%s]: ", defaultUID)
	} else {
		fmt.Print("Platform UID: ")
	}

	// 逐字节读直到换行: 不能用 bufio.Reader(预读会吞掉管道中
	// 后续命令行, 而 REPL(liner)从同一 fd 读取时已无数据)。
	var sb strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				break
			}
			sb.WriteByte(buf[0])
		}
		if err != nil {
			break
		}
	}
	input := strings.TrimSpace(sb.String())

	if input == "" {
		if defaultUID == "" {
			fmt.Println("error: platform_uid required")
			os.Exit(1)
		}
		return defaultUID
	}
	return input
}
