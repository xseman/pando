//go:build unix

// pando: a terminal sidebar (files, git, agent sessions per worktree) with a
// daemon-backed CLI API.
package main

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/xseman/pando/internal/daemon"
	"github.com/xseman/pando/internal/proto"
	"github.com/xseman/pando/internal/ui"
	"github.com/xseman/pando/internal/update"
)

const usage = `pando — terminal sidebar with agent sessions

  pando                         open the TUI in the project opened last
  pando DIR                     add DIR to the projects and open it
  pando serve                   run the daemon in the foreground
  pando stop                    stop the daemon (sessions are respawned next start)
  pando doctor                  show the config path, the UI icons and the fonts that draw them
  pando version                 print the version this binary was built from
  pando update                  install the latest release over this binary
  pando call METHOD [JSON]      raw API call, prints the JSON result
  pando skill                   print the guide to driving pando from an agent

  pando project add|rm [DIR] | move DIR INDEX | ls
  pando ws ls | new [BRANCH] [--project DIR] | rm PATH | switch PATH
  pando session new [--agent NAME] [--ws PATH] [--name NAME] [--parent ID] [--wait] [-- CMD...]
  pando session ls | get ID | kill ID | switch ID | rename ID [NAME...] | move ID OTHER
  pando session send ID TEXT... [--enter] [--wait]
  pando session keys ID KEY...  named keys, e.g. esc ctrl+c shift+tab
  pando session wait ID [--until idle|blocked|running|exited] [--match RE] [--timeout MS]
  pando session read ID [--scrollback] [--lines N]
  pando open FILE               show FILE in every attached TUI
  pando set KEY VALUE           change a setting from config.toml, e.g. pando set icons ascii

ID is a session id, the name "session new --name" or "session rename" gave it,
or an unambiguous prefix of either.

Driving pando from an agent: "pando skill" prints skills/pando/SKILL.md, the
guide to doing it well. Read it before the first call.

API methods: ping state.get state.set draft.list draft.set project.add
project.remove project.move workspace.list workspace.new workspace.remove focus session.new
session.get session.list session.kill session.rename session.move session.input session.screen
session.read session.wait update.status update.check update.install subscribe shutdown

The API covers sessions, workspaces, projects and settings — everything a
script needs to drive pando. Explorer, Source Control, Search and the editor
are TUI surfaces over git and the filesystem; use git and your own tools for
those.
`

// skill is the guide to driving pando from an agent, printed by `pando skill`
// so the binary carries it wherever it is installed.
//
//go:embed skills/pando/SKILL.md
var skill string

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, "pando:", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return ui.Run("") // no directory: the workspace shown last reopens
	}

	cmd, rest := args[0], args[1:]
	switch cmd {
	case "serve":
		return serve()
	case "version", "--version", "-v":
		fmt.Println("pando", update.Version)
		return nil

	case "doctor":
		fmt.Printf("pando %s\nconfig: %s\n\n", update.Version, filepath.Join(daemon.ConfigDir(), "config.toml"))
		ui.Doctor(os.Stdout)

		cwd, _ := os.Getwd()
		ui.CommandIDs(os.Stdout, cwd)

		return nil

	case "help", "-h", "--help":
		fmt.Print(usage)
		return nil

	case "skill":
		fmt.Print(skill)
		return nil
	}

	if err := proto.EnsureDaemon(); err != nil {
		return err
	}

	switch cmd {
	case "call":
		if len(rest) == 0 {
			return errors.New("call METHOD [JSON]")
		}

		var params any
		if len(rest) > 1 {
			params = json.RawMessage(rest[1])
		}

		return printJSON(rest[0], params)

	case "stop":
		return proto.Call("shutdown", nil, nil)
	case "update":
		return selfUpdate()
	case "open":
		if len(rest) != 1 {
			return errors.New("open FILE")
		}

		return proto.Call("focus", proto.FocusParams{Open: abs(rest[0])}, nil)

	case "set":
		if len(rest) != 2 {
			return errors.New("set KEY VALUE")
		}

		var v any = rest[1]

		_ = json.Unmarshal([]byte(rest[1]), &v) // numbers and booleans, else the string

		return proto.Call("state.set", map[string]any{"settings": map[string]any{rest[0]: v}}, nil)

	case "project":
		return project(rest)
	case "ws", "workspace":
		return workspace(rest)
	case "session", "s":
		return session(rest)
	}

	// Not a command: pando DIR adds DIR to the projects and opens it.
	if len(rest) == 0 {
		if st, err := os.Stat(cmd); err == nil && st.IsDir() {
			return ui.Run(abs(cmd))
		}

		if strings.ContainsRune(cmd, filepath.Separator) || cmd == "." || cmd == ".." {
			return fmt.Errorf("%s is not a directory", cmd)
		}
	}

	return fmt.Errorf("unknown command %q (see pando help)", cmd)
}

// selfUpdate installs the newest release over this binary, through the
// daemon, so the TUI's chip and this command follow the same download.
func selfUpdate() error {
	var u proto.Update
	if err := proto.Call("update.check", nil, &u); err != nil {
		return err
	}

	switch {
	case u.State == "ready":
		fmt.Printf("pando %s is installed already, restart pando to run it\n", u.Latest)
		return nil

	// A build without a version installs the latest release: it is the way
	// back from a local `make install` to a released pando.
	case u.State == "current" && u.Current != update.Dev:
		fmt.Printf("pando %s is the latest release\n", u.Current)
		return nil
	}
	// Subscribed before the install starts, so no progress is missed. The
	// stream is abandoned on the way out; the process exits with it.
	events, err := proto.Subscribe()
	if err != nil {
		return err
	}

	fmt.Printf("pando %s → %s\n", u.Current, u.Latest)

	if err := proto.Call("update.install", nil, nil); err != nil {
		return err
	}

	for ev := range events {
		if ev.Kind != "update" {
			continue
		}

		var p proto.Update
		if json.Unmarshal(ev.Data, &p) != nil {
			continue
		}

		switch p.State {
		case "downloading":
			fmt.Printf("\r  %3d%%", p.Percent())
		case "ready":
			fmt.Printf("\r  installed, restart pando to run %s\n", p.Latest)
			return nil

		case "error":
			fmt.Println()
			return errors.New(p.Error)
		}
	}

	return errors.New("the daemon closed the connection")
}

// printJSON makes one call and prints its result, indented.
func printJSON(method string, params any) error {
	var out json.RawMessage
	if err := proto.Call(method, params, &out); err != nil {
		return err
	}

	var b bytes.Buffer

	_ = json.Indent(&b, out, "", "  ") // the daemon answers JSON
	fmt.Println(b.String())

	return nil
}

// out prints a value as the indented JSON the API commands answer with.
func out(v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}

	fmt.Println(string(b))

	return nil
}

func abs(p string) string {
	if a, err := filepath.Abs(p); err == nil {
		return a
	}

	return p
}

func arg(rest []string, i int, def string) string {
	if i < len(rest) {
		return rest[i]
	}

	return def
}

func project(rest []string) error {
	switch arg(rest, 0, "ls") {
	case "ls":
		var st proto.State
		if err := proto.Call("state.get", nil, &st); err != nil {
			return err
		}

		for _, p := range st.Projects {
			fmt.Println(p)
		}

		return nil

	case "add":
		return printJSON("project.add", map[string]string{"path": abs(arg(rest, 1, "."))})
	case "rm":
		return proto.Call("project.remove", map[string]string{"path": abs(arg(rest, 1, "."))}, nil)
	case "move", "mv":
		to, err := strconv.Atoi(arg(rest, 2, ""))
		if err != nil {
			return errors.New("project move DIR INDEX (0 is the top of the list)")
		}

		return proto.Call("project.move", proto.MoveParams{Path: abs(arg(rest, 1, ".")), To: to}, nil)
	}

	return errors.New("project add|rm [DIR] | move DIR INDEX | ls")
}

func workspace(rest []string) error {
	fs := flag.NewFlagSet("ws", flag.ContinueOnError)
	projectDir := fs.String("project", ".", "project directory")

	pos, err := parse(fs, rest[min(1, len(rest)):])
	if err != nil {
		return err
	}

	switch arg(rest, 0, "ls") {
	case "ls":
		return printJSON("workspace.list", nil)
	case "new":
		var root string
		if err := proto.Call("project.add", map[string]string{"path": abs(*projectDir)}, &root); err != nil {
			return err
		}

		if len(pos) > 1 {
			return errors.New("ws new [BRANCH] [--project DIR]")
		}

		return printJSON("workspace.new", map[string]string{"project": root, "branch": arg(pos, 0, "")}) // "": a random one

	case "rm":
		return proto.Call("workspace.remove", map[string]string{"path": abs(arg(pos, 0, ""))}, nil)

	case "switch":
		return proto.Call("focus", proto.FocusParams{Workspace: abs(arg(pos, 0, ""))}, nil)
	}

	return errors.New("ws ls | new [BRANCH] | rm PATH | switch PATH")
}

// parse reads flags from anywhere among the positionals, so `new feat --project x`
// works; `--` ends the flags and everything after it is positional.
func parse(fs *flag.FlagSet, args []string) ([]string, error) {
	var pos []string

	for len(args) > 0 {
		if err := fs.Parse(args); err != nil {
			return nil, err
		}

		rest := fs.Args()
		if n := len(args) - len(rest); len(rest) == 0 || (n > 0 && args[n-1] == "--") {
			return append(pos, rest...), nil
		}

		pos, args = append(pos, rest[0]), rest[1:]
	}

	return pos, nil
}

// waitFor blocks on session.wait; until is the comma-separated list the flag
// takes, empty for the settled statuses.
func waitFor(id, until, match string, timeout int) (proto.Session, error) {
	p := proto.WaitParams{ID: id, Match: match, Timeout: timeout}
	if until != "" {
		p.Until = strings.Split(until, ",")
	}

	var s proto.Session

	err := proto.Call("session.wait", p, &s)

	return s, err
}

func session(rest []string) error {
	fs := flag.NewFlagSet("session", flag.ContinueOnError)
	agent := fs.String("agent", "", "agent preset (claude, codex, gemini, opencode, shell)")
	ws := fs.String("ws", ".", "workspace directory")
	name := fs.String("name", "", "name the session answers to")
	parent := fs.String("parent", "", "the session whose Terminal panel a --agent terminal shell joins")
	enter := fs.Bool("enter", false, "press enter after the text")
	wait := fs.Bool("wait", false, "block until the session settles; send implies --enter")
	until := fs.String("until", "", "statuses to wait for, comma separated (default idle,blocked,exited)")
	match := fs.String("match", "", "regular expression the screen must match")
	timeout := fs.Int("timeout", 0, "milliseconds before a wait gives up, 0 waits forever")
	scrollback := fs.Bool("scrollback", false, "include scrollback")
	lines := fs.Int("lines", 0, "keep only the last N lines")

	pos, err := parse(fs, rest[min(1, len(rest)):])
	if err != nil {
		return err
	}

	switch arg(rest, 0, "ls") {
	case "ls":
		return printJSON("session.list", nil)
	case "get":
		return printJSON("session.get", map[string]string{"id": arg(pos, 0, "")})
	case "new":
		if *agent == "" && len(pos) == 0 {
			*agent = "shell"
		}

		var s proto.Session

		p := map[string]any{"workspace": abs(*ws), "agent": *agent, "name": *name, "parent": *parent, "cmd": pos}
		if err := proto.Call("session.new", p, &s); err != nil {
			return err
		}

		if !*wait {
			return out(s)
		}
		// An agent's TUI is not ready for a prompt until it has drawn itself;
		// a session starts busy, so the settled wait needs no activity gate.
		if s, err = waitFor(s.ID, *until, *match, *timeout); err != nil {
			return err
		}

		return out(s)

	case "kill":
		return proto.Call("session.kill", map[string]string{"id": arg(pos, 0, "")}, nil)
	case "rename": // no name: named by what runs in it again
		return proto.Call("session.rename", map[string]string{"id": arg(pos, 0, ""), "name": strings.Join(pos[min(1, len(pos)):], " ")}, nil)
	case "move", "mv":
		if len(pos) != 2 {
			return errors.New("session move ID OTHER (ID takes OTHER's place in its worktree)")
		}

		return proto.Call("session.move", proto.SessionMoveParams{ID: pos[0], To: pos[1]}, nil)

	case "switch":
		return proto.Call("focus", proto.FocusParams{Session: arg(pos, 0, "")}, nil)
	case "send":
		if len(pos) < 2 {
			return errors.New("session send ID TEXT... [--enter] [--wait]")
		}

		id, text := pos[0], strings.Join(pos[1:], " ")

		in := proto.InputParams{ID: id, Text: text}
		if strings.Contains(text, "\n") {
			// Typed line by line a multi-line prompt submits at its first
			// newline; a paste arrives as one block wherever the app brackets it.
			in = proto.InputParams{ID: id, Paste: text}
		}

		if err := proto.Call("session.input", in, nil); err != nil {
			return err
		}

		if !*enter && !*wait { // waiting for a prompt never submitted only times out
			return nil
		}
		// A separate, slightly later Enter: line editors (ble.sh, some agent
		// prompts) treat "\r" inside a fast burst as a pasted newline.
		time.Sleep(50 * time.Millisecond)

		if err := proto.Call("session.input", proto.InputParams{ID: id, Text: "\r"}, nil); err != nil || !*wait {
			return err
		}
		// A turn takes a moment to start, and the settled wait would otherwise
		// return the idle the prompt was typed into. Give the session a window
		// to pick the work up; a turn that finishes inside it settles anyway,
		// so a timeout here is not an error.
		_, _ = waitFor(id, "running,blocked,exited", "", 3000)

		s, err := waitFor(id, *until, *match, *timeout)
		if err != nil {
			return err
		}

		return out(s)

	case "keys":
		if len(pos) < 2 {
			return fmt.Errorf("session keys ID KEY...\nkeys: %s\nprefixes: alt+ ctrl+ meta+ shift+ super+, any other character is itself",
				strings.Join(proto.KeyNames(), " "))
		}

		keys := make([]proto.Key, 0, len(pos)-1)
		for _, n := range pos[1:] {
			k, err := proto.ParseKey(n)
			if err != nil {
				return err // parsed before any is sent: a typo late in the list delivers nothing
			}

			keys = append(keys, k)
		}

		return proto.Call("session.input", proto.InputParams{ID: pos[0], Keys: keys}, nil)

	case "wait":
		s, err := waitFor(arg(pos, 0, ""), *until, *match, *timeout)
		if err != nil {
			return err
		}

		return out(s)

	case "read":
		var text string

		p := proto.ReadParams{ID: arg(pos, 0, ""), Scrollback: *scrollback, Lines: *lines}
		if err := proto.Call("session.read", p, &text); err != nil {
			return err
		}

		fmt.Println(text)

		return nil
	}

	return errors.New("session new | ls | get | kill | switch | rename | send | keys | wait | read")
}

func serve() error {
	dir := proto.Dir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}

	lock, err := os.OpenFile(filepath.Join(dir, "pando.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}

	defer func() { _ = lock.Close() }()

	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return errors.New("another pando daemon is running")
	}

	if len(proto.SocketPath()) > 104 { // sun_path limit (104 on darwin, 108 on linux)
		return fmt.Errorf("socket path %s is too long, set PANDO_RUNTIME_DIR to a shorter directory", proto.SocketPath())
	}

	_ = os.Remove(proto.SocketPath()) // a socket left behind by a crash

	ln, err := net.Listen("unix", proto.SocketPath())
	if err != nil {
		return err
	}

	d, err := daemon.New(daemon.ConfigDir(), daemon.DataDir())
	if err != nil {
		_ = ln.Close()
		return err
	}

	go d.WatchUpdates()

	sig := make(chan os.Signal, 1)

	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	go func() {
		<-sig
		d.Close()
	}()

	err = d.Serve(ln)
	d.Close()

	return err
}
