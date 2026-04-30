package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"hy_client/pkg/client"
)

type REPL struct {
	client *client.Client
	reader *bufio.Reader
}

func NewREPL(c *client.Client) *REPL {
	return &REPL{
		client: c,
		reader: bufio.NewReader(os.Stdin),
	}
}

func (r *REPL) Run() {
	fmt.Println("Type 'help' for available commands, 'quit' to exit.")

	for {
		fmt.Print("hy> ")
		line, err := r.reader.ReadString('\n')
		if err != nil {
			fmt.Printf("\nread error: %v\n", err)
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		switch line {
		case "quit", "exit":
			fmt.Println("bye.")
			return
		case "help":
			r.printHelp()
		case "reconnect":
			r.reconnect()
		default:
			r.execute(line)
		}
	}
}

func (r *REPL) execute(line string) {
	parts := parseArgs(line)
	if len(parts) == 0 {
		return
	}

	name := parts[0]
	args := parts[1:]

	// reconnect is the only command that works when disconnected
	if name != "reconnect" && name != "help" && name != "quit" && name != "exit" && !r.client.IsConnected() {
		fmt.Println("disconnected from server. type 'reconnect' to reconnect.")
		return
	}

	cmd, ok := commands[name]
	if !ok {
		fmt.Printf("unknown command: %s (type 'help' for commands)\n", name)
		return
	}

	if err := cmd.Exec(r.client, args); err != nil {
		fmt.Printf("error: %v\n", err)
	}
}

// parseArgs splits a line into arguments, respecting double-quoted strings.
// Example: gm.cmd "add item 1001 10" → ["gm.cmd", "add item 1001 10"]
func parseArgs(line string) []string {
	var args []string
	var current strings.Builder
	inQuote := false

	for i := 0; i < len(line); i++ {
		ch := line[i]

		if ch == '"' {
			if inQuote {
				inQuote = false
			} else {
				inQuote = true
			}
			continue
		}

		if ch == ' ' && !inQuote {
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
			continue
		}

		current.WriteByte(ch)
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}

	return args
}

func (r *REPL) printHelp() {
	fmt.Println("Available commands:")
	fmt.Println("  help                    Show this help")
	fmt.Println("  quit                    Exit")
	fmt.Println("  reconnect               Reconnect to server")
	fmt.Println("")
	for name, cmd := range commands {
		paramsStr := strings.Join(cmd.Params, " ")
		if paramsStr != "" {
			fmt.Printf("  %-25s %s (%s)\n", name, cmd.Help, paramsStr)
		} else {
			fmt.Printf("  %-25s %s\n", name, cmd.Help)
		}
	}
}

func (r *REPL) reconnect() {
	r.client.Close()
	fmt.Println("disconnected. reconnecting...")
	if err := r.client.Connect(); err != nil {
		fmt.Printf("connect failed: %v\n", err)
		return
	}
	fmt.Println("connected.")

	rsp, err := r.client.Handshake()
	if err != nil {
		fmt.Printf("handshake failed: %v\n", err)
		return
	}
	fmt.Printf("handshake ok, role_id=%d\n", rsp.RoleId)

	_, err = r.client.Login()
	if err != nil {
		fmt.Printf("login failed: %v\n", err)
		return
	}
	fmt.Println("login ok.")
}
