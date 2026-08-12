package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/DGS-ai-team/DAgents/client/internal/probe"
	"github.com/DGS-ai-team/DAgents/client/internal/update"
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
	case "update":
		return cmdUpdate(*configPath, rest)
	case "chat", "tui":
		fmt.Fprintf(os.Stderr, "command %q removed: use Web UI at http://127.0.0.1:<listen.port>/ui/ (start with: dagents node)\n", cmd)
		return 2
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q (supported: probe, update, version)\n", cmd)
		return 2
	}
}

func cmdUpdate(configPath string, args []string) int {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	checkOnly := fs.Bool("check", false, "only check for updates")
	force := fs.Bool("force", false, "skip confirmation prompt")
	output := fs.String("output", "", "download package to this path")
	if err := fs.Parse(args); err != nil {
		return 2
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	return update.Run(ctx, cfg, update.Options{
		CheckOnly: *checkOnly,
		Force:     *force,
		Output:    *output,
	})
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
		res.Endpoint, res.NodeID, res.Version, res.Capabilities, res.ManageRegistered)
	if res.ProfilePending {
		fmt.Printf("note: node profile onboarding pending (open Web UI to finish LLM setup)\n")
	}
	return 0
}
