package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"hy_client/pkg/client"

	"github.com/peterh/liner"
)

type REPL struct {
	client *client.Client
	line   *liner.State
}

func historyFile() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".hy_history"
	}
	return filepath.Join(home, ".hy_history")
}

func NewREPL(c *client.Client) *REPL {
	line := liner.NewLiner()
	line.SetCtrlCAborts(true)

	if f, err := os.Open(historyFile()); err == nil {
		line.ReadHistory(f)
		f.Close()
	}

	return &REPL{
		client: c,
		line:   line,
	}
}

func (r *REPL) Run() {
	defer func() {
		if f, err := os.Create(historyFile()); err == nil {
			r.line.WriteHistory(f)
			f.Close()
		}
		r.line.Close()
	}()

	fmt.Println("Type 'help' for available commands, 'quit' to exit.")

	for {
		line, err := r.line.Prompt("hy> ")
		if err != nil {
			if err == liner.ErrPromptAborted {
				fmt.Println("\nbye.")
				return
			}
			fmt.Printf("\nerror: %v\n", err)
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		r.line.AppendHistory(line)

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
	fmt.Println("Commands:")
	fmt.Println("  help          Show this help")
	fmt.Println("  quit          Exit")
	fmt.Println("  reconnect     Reconnect to server")

	// Group commands by category
	grouped := make(map[string][]string)
	var groupOrder []string
	seen := make(map[string]bool)
	for _, name := range commandOrder {
		g := groupOf(name)
		grouped[g] = append(grouped[g], name)
		if !seen[g] {
			seen[g] = true
			groupOrder = append(groupOrder, g)
		}
	}

	for _, g := range groupOrder {
		fmt.Printf("\n[%s]\n", g)
		for _, name := range grouped[g] {
			cmd := commands[name]
			paramsStr := strings.Join(cmd.Params, " ")
			if paramsStr != "" {
				fmt.Printf("  %-25s %s (%s)\n", name, cmd.Help, paramsStr)
			} else {
				fmt.Printf("  %-25s %s\n", name, cmd.Help)
			}
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
