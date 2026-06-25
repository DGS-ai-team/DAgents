package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	nodeapi "github.com/DGS-ai-team/DAgents/client/internal/api"
	clihitl "github.com/DGS-ai-team/DAgents/client/internal/hitl"
	"github.com/DGS-ai-team/DAgents/client/internal/probe"
	"github.com/DGS-ai-team/DAgents/client/internal/tui"
	"github.com/DGS-ai-team/DAgents/shared/config"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) > 0 && (args[0] == "version" || args[0] == "-version" || args[0] == "--version") {
		return cmdVersion("")
	}
	fs := flag.NewFlagSet("dagents-client", flag.ExitOnError)
	configPath := fs.String("config", "", "path to config.yaml (optional; default: DAGENTS_CONFIG or packaging/agent-client/config.yaml)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	rest := fs.Args()
	cmd := "probe"
	if len(rest) > 0 {
		cmd = rest[0]
		rest = rest[1:]
	}

	switch cmd {
	case "probe":
		return cmdProbe(*configPath)
	case "chat":
		return cmdChat(*configPath, rest)
	case "tui":
		return cmdTUI(*configPath, rest)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (supported: probe, chat, tui)\n", cmd)
		return 2
	}
}

func cmdVersion(configPath string) int {
	resolved, err := config.ResolveConfigPath(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "version unavailable: %v\n", err)
		return 1
	}
	cfg, err := config.LoadFile(resolved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "version unavailable: %v\n", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	res, err := probe.Node(ctx, cfg, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "version unavailable: probe failed: %v\n", err)
		return 1
	}
	fmt.Println(res.Version)
	return 0
}

func cmdProbe(configPath string) int {
	resolved, err := config.ResolveConfigPath(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 2
	}

	cfg, err := config.LoadFile(resolved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	res, err := probe.Node(ctx, cfg, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "probe failed: %v\n", err)
		return 1
	}

	fmt.Printf("ok endpoint=%s agent_id=%s version=%s capabilities=%v manage_registered=%v\n",
		res.Endpoint, res.AgentID, res.Version, res.Capabilities, res.ManageRegistered)
	return 0
}

func cmdChat(configPath string, args []string) int {
	if len(args) == 0 {
		return cmdTUI(configPath, nil)
	}
	content := strings.Join(args, " ")

	resolved, err := config.ResolveConfigPath(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 2
	}

	cfg, err := config.LoadFile(resolved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	client := nodeapi.New(cfg.Local.Endpoint, nil)
	sessionID, err := client.CreateSession(ctx, "")
	if err != nil {
		fmt.Fprintf(os.Stderr, "create session: %v\n", err)
		return 1
	}

	var wg sync.WaitGroup
	var streamErr error
	doneCh := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		streamErr = client.StreamEvents(ctx, sessionID, 0, func(ev nodeapi.StreamEvent) bool {
			cont, err := clihitl.HandleStreamEvent(ctx, client, sessionID, ev, clihitl.Sink{}, nil, true)
			if err != nil {
				fmt.Fprintf(os.Stderr, "hitl: %v\n", err)
				return false
			}
			if !cont {
				close(doneCh)
				return false
			}
			return true
		})
	}()

	time.Sleep(100 * time.Millisecond)
	if err := client.SubmitMessage(ctx, sessionID, content); err != nil {
		fmt.Fprintf(os.Stderr, "submit message: %v\n", err)
		cancel()
		wg.Wait()
		return 1
	}

	<-doneCh
	cancel()
	wg.Wait()
	if streamErr != nil && streamErr != context.Canceled {
		fmt.Fprintf(os.Stderr, "stream: %v\n", streamErr)
		return 1
	}
	fmt.Println()
	return 0
}

func cmdTUI(configPath string, args []string) int {
	resolved, err := config.ResolveConfigPath(configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 2
	}
	cfg, err := config.LoadFile(resolved)
	if err != nil {
		fmt.Fprintf(os.Stderr, "config: %v\n", err)
		return 1
	}
	initialSession := ""
	plain := false
	forceFull := false
	showReasoning := false
	for _, arg := range args {
		switch arg {
		case "--plain", "-plain":
			plain = true
		case "--full":
			forceFull = true
		case "--show-reasoning":
			showReasoning = true
		default:
			if initialSession == "" {
				initialSession = arg
			}
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := tui.Run(ctx, cfg, initialSession, tui.Options{
		Plain:         plain,
		ForceFull:     forceFull,
		ShowReasoning: showReasoning,
	}); err != nil {
		fmt.Fprintf(os.Stderr, "tui: %v\n", err)
		return 1
	}
	return 0
}
