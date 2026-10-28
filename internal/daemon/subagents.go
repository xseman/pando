package daemon

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// subagentReaders read the subagents an agent's process runs now, oldest
// first, by program: pid is the agent's process, c the conversation remember
// found for it. Gemini and opencode report none.
var subagentReaders = map[string]func(pid int, c *proto.Conversation, now time.Time) []proto.Subagent{
	"claude": func(_ int, c *proto.Conversation, now time.Time) []proto.Subagent {
		if c == nil {
			return nil
		}

		return claudeSubagents(*c, now)
	},
	"codex": func(pid int, _ *proto.Conversation, _ time.Time) []proto.Subagent {
		return codexSubagents(openFiles(pid))
	},
	"agy": func(pid int, _ *proto.Conversation, _ time.Time) []proto.Subagent {
		return agySubagents(openFiles(pid))
	},
	"grok": func(pid int, _ *proto.Conversation, _ time.Time) []proto.Subagent {
		return grokSubagents(openFiles(pid))
	},
}

func byStart(out []proto.Subagent) []proto.Subagent {
	slices.SortFunc(out, func(a, b proto.Subagent) int { return a.Started.Compare(b.Started) })

	return out
}

// subagentStale is how long an unfinished subagent's files may go unwritten
// before it counts as one a crash left: claude writes nothing while a tool
// call runs, and caps one at 10 minutes.
// ponytail: a subagent in one call longer than this drops off the list, and
// a crash's leftover stays on it this long after a resume.
const subagentStale = 15 * time.Minute

// claudeSubagents reads the transcript's subagents directory: claude writes
// agent-<id>.meta.json as a subagent starts and rewrites it with "finished"
// when it ends. A finished one is not read again.
func claudeSubagents(c proto.Conversation, now time.Time) []proto.Subagent {
	subs.Lock()
	defer subs.Unlock()

	sc := subs.m[c.ID]
	if sc == nil {
		m, _ := filepath.Glob(filepath.Join(claudeDirIn(c.Env), "projects", "*", c.ID+".jsonl"))
		if len(m) == 0 {
			return nil
		}

		sc = &subScan{dir: filepath.Join(strings.TrimSuffix(m[0], ".jsonl"), "subagents"), done: map[string]bool{}}
		subs.m[c.ID] = sc
	}

	es, _ := os.ReadDir(sc.dir) // none yet: the directory comes with the first

	var out []proto.Subagent

	for _, e := range es {
		name, ok := strings.CutSuffix(e.Name(), ".meta.json")
		if !ok || sc.done[name] {
			continue
		}

		meta, err := e.Info()
		if err != nil {
			continue
		}

		b, err := os.ReadFile(filepath.Join(sc.dir, e.Name()))

		var v struct {
			Type        string `json:"agentType"`
			Description string `json:"description"`
			Finished    bool   `json:"finished"`
		}

		if err != nil || json.Unmarshal(b, &v) != nil { // caught half written: read again next tick
			continue
		}

		last := meta.ModTime()
		if t, err := os.Stat(filepath.Join(sc.dir, name+".jsonl")); err == nil && t.ModTime().After(last) { // written a moment after the meta
			last = t.ModTime()
		}

		if v.Finished {
			sc.done[name] = true
			continue
		}

		if now.Sub(last) > subagentStale { // back on the list once it writes again
			continue
		}

		out = append(out, proto.Subagent{Type: v.Type, Description: v.Description, Started: meta.ModTime()})
	}

	slices.SortFunc(out, func(a, b proto.Subagent) int { return a.Started.Compare(b.Started) })

	return out
}

// subs are the subagents directories subagents has read, by conversation.
var subs = struct {
	sync.Mutex
	m map[string]*subScan
}{m: map[string]*subScan{}}

// subScan is a conversation's subagents directory and the subagents in it
// that ended.
type subScan struct {
	dir  string
	done map[string]bool
}

// openFiles are the files process pid and its descendants hold open, where
// /proc says (Linux): codex runs under node, its own binary a child of it.
func openFiles(pid int) []string {
	if pid <= 0 {
		return nil
	}

	var out []string

	for todo := []int{pid}; len(todo) > 0; {
		p := todo[len(todo)-1]
		todo = todo[:len(todo)-1]

		dir := "/proc/" + strconv.Itoa(p) + "/fd/"
		fds, _ := os.ReadDir(dir) // gone, or not on Linux: nothing to read

		for _, fd := range fds {
			if l, err := os.Readlink(dir + fd.Name()); err == nil && filepath.IsAbs(l) {
				out = append(out, l)
			}
		}

		todo = append(todo, childrenOf(p)...)
	}

	return out
}

// codexSubagents are the subagent threads among the rollouts open in files.
// Codex writes each thread to a rollout-…-<id>.jsonl and keeps it open while
// the thread lives; a subagent's session_meta names the thread that spawned
// it (source.subagent.thread_spawn). One works from its turn's task_started
// to its task_complete or turn_aborted.
func codexSubagents(files []string) []proto.Subagent {
	codexThreads.Lock()
	defer codexThreads.Unlock()

	var out []proto.Subagent

	for _, p := range files {
		if base := filepath.Base(p); !strings.HasPrefix(base, "rollout-") || filepath.Ext(base) != ".jsonl" {
			continue
		}

		t := codexThreads.m[p]
		if t == nil {
			if t = codexMeta(p); t == nil { // its first line still being written: read again next tick
				continue
			}

			codexThreads.m[p] = t
		}

		if t.sub.Type == "" { // the conversation itself
			continue
		}

		t.read(p)

		if t.running {
			out = append(out, t.sub)
		}
	}

	return byStart(out)
}

// codexThreads are the rollouts codexSubagents has read, by path.
var codexThreads = struct {
	sync.Mutex
	m map[string]*codexThread
}{m: map[string]*codexThread{}}

// codexThread is a rollout as codexSubagents knows it: the subagent it
// writes (no Type for one that is none), how far it was read, and whether
// its turn was going at that point.
type codexThread struct {
	sub     proto.Subagent
	off     int64
	running bool
}

// codexMeta reads rollout path's first line, its session_meta; nil while it
// is not whole yet.
func codexMeta(path string) *codexThread {
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer func() { _ = f.Close() }() // read only: nothing to lose

	line, err := bufio.NewReader(f).ReadBytes('\n') // whole instructions in it: tens of kilobytes
	if err != nil {
		return nil
	}

	var v struct {
		Payload struct {
			Timestamp time.Time       `json:"timestamp"`
			Nickname  string          `json:"agent_nickname"`
			Path      string          `json:"agent_path"`
			Source    json.RawMessage `json:"source"`
		} `json:"payload"`
	}

	if json.Unmarshal(line, &v) != nil {
		return nil
	}

	var src struct { // a string ("exec", "cli") for a thread no agent spawned
		Subagent struct {
			ThreadSpawn *struct{} `json:"thread_spawn"`
		} `json:"subagent"`
	}

	t := &codexThread{}
	if json.Unmarshal(v.Payload.Source, &src) == nil && src.Subagent.ThreadSpawn != nil {
		// "/root/find_callers" is the task the parent named, "Harvey" the
		// name codex gave the agent doing it.
		t.sub = proto.Subagent{
			Type:        cmp.Or(v.Payload.Nickname, "agent"),
			Description: strings.ReplaceAll(filepath.Base(cmp.Or(v.Payload.Path, ".")), "_", " "),
			Started:     v.Payload.Timestamp,
		}
	}

	return t
}

// read takes in the whole lines appended since the last read, for the
// turn's last task event; a file that shrank is read again from the start.
func (t *codexThread) read(path string) {
	f, err := os.Open(path)
	if err != nil {
		return
	}
	defer func() { _ = f.Close() }() // read only: nothing to lose

	st, err := f.Stat()
	if err != nil {
		return
	}

	if st.Size() < t.off {
		t.off, t.running = 0, false
	}

	if st.Size() == t.off {
		return
	}

	b := make([]byte, st.Size()-t.off)
	n, _ := f.ReadAt(b, t.off)

	end := bytes.LastIndexByte(b[:n], '\n')
	if end < 0 { // a line still being written: it is read whole next time
		return
	}

	for line := range bytes.SplitSeq(b[:end], []byte{'\n'}) {
		if !bytes.Contains(line, []byte(`"type":"event_msg"`)) { // the rest hold whole tool output
			continue
		}

		var v struct {
			Payload struct {
				Type string `json:"type"`
			} `json:"payload"`
		}

		if json.Unmarshal(line, &v) != nil {
			continue
		}

		switch v.Payload.Type {
		case "task_started":
			t.running = true
		case "task_complete", "turn_aborted":
			t.running = false
		}
	}

	t.off += int64(end + 1)
}

// agySubagents are the subagents of the conversation an agy process has
// open, the one whose presence/<id>.lock it holds in files. agy keeps
// brain/<id>/.system_generated/subagents/<child>.json for each subagent it
// started, ALIVE until killed: one can be sent more work. An alive one works
// while its transcript's last step leaves its turn going.
// ponytail: one waiting on a background command of its own has ended its
// turn, and reads as done until agy wakes it.
func agySubagents(files []string) []proto.Subagent {
	var out []proto.Subagent

	for _, p := range files {
		if filepath.Base(filepath.Dir(p)) != "presence" || filepath.Ext(p) != ".lock" {
			continue
		}

		home, id := filepath.Dir(filepath.Dir(p)), strings.TrimSuffix(filepath.Base(p), ".lock")
		dir := filepath.Join(home, "brain", id, ".system_generated", "subagents")
		es, _ := os.ReadDir(dir) // none yet: the directory comes with the first

		for _, e := range es {
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))

			var v struct {
				ID         string `json:"conversationId"`
				Descriptor struct {
					Type string `json:"typeName"`
					Role string `json:"role"`
				} `json:"subagentDescriptor"`
				State string `json:"state"`
			}

			if err != nil || json.Unmarshal(b, &v) != nil || v.State != "SUBAGENT_STATE_ALIVE" {
				continue
			}

			info, err := e.Info()
			if err != nil || !agyWorking(filepath.Join(home, "brain", v.ID, ".system_generated", "logs", "transcript.jsonl")) {
				continue
			}

			typ := v.Descriptor.Type
			if typ == "self" { // a copy of the agent that started it
				typ = "subagent"
			}

			out = append(out, proto.Subagent{Type: typ, Description: v.Descriptor.Role, Started: info.ModTime()})
		}
	}

	return byStart(out)
}

// grokSubagents are the subagents of the sessions a grok process holds
// open in files, by each one's events.jsonl. Grok keeps
// subagents/<id>/meta.json beside a session's for each subagent it spawned,
// "running" until it ends, and the subagent's own session open meanwhile:
// one a crash left "running" is not open, so it is left out.
func grokSubagents(files []string) []proto.Subagent {
	open := map[string]string{} // session id to its directory

	for _, p := range files {
		if filepath.Base(p) == "events.jsonl" {
			open[filepath.Base(filepath.Dir(p))] = filepath.Dir(p)
		}
	}

	var out []proto.Subagent

	for _, dir := range open {
		metas, _ := filepath.Glob(filepath.Join(dir, "subagents", "*", "meta.json"))

		for _, m := range metas {
			b, err := os.ReadFile(m)

			var v struct {
				Child       string    `json:"child_session_id"`
				Type        string    `json:"subagent_type"`
				Description string    `json:"description"`
				Status      string    `json:"status"`
				Started     time.Time `json:"started_at"`
			}

			if err != nil || json.Unmarshal(b, &v) != nil || v.Status != "running" || open[v.Child] == "" {
				continue
			}

			out = append(out, proto.Subagent{Type: v.Type, Description: v.Description, Started: v.Started})
		}
	}

	return byStart(out)
}

// agyTail is how much of a transcript's end agyWorking reads for its last
// step; a longer step, a final answer of over a megabyte, reads as going.
const agyTail = 1 << 20

// agyWorking reports whether the last step in agy transcript path leaves
// its turn going: anything but a finished model reply that calls no tool.
// A transcript is read again only once it grew.
func agyWorking(path string) bool {
	agyTurns.Lock()
	defer agyTurns.Unlock()

	st, err := os.Stat(path)
	if err != nil {
		return false
	}

	if t, ok := agyTurns.m[path]; ok && t.size == st.Size() {
		return t.working
	}

	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer func() { _ = f.Close() }() // read only: nothing to lose

	off := max(st.Size()-agyTail, 0)
	b := make([]byte, st.Size()-off)
	n, _ := f.ReadAt(b, off)

	b = bytes.TrimRight(b[:n], "\n")
	line := b[bytes.LastIndexByte(b, '\n')+1:]

	var v struct {
		Type      string            `json:"type"`
		Status    string            `json:"status"`
		ToolCalls []json.RawMessage `json:"tool_calls"`
	}

	working := json.Unmarshal(line, &v) != nil || v.Type != "PLANNER_RESPONSE" || v.Status != "DONE" || len(v.ToolCalls) > 0
	agyTurns.m[path] = agyTurn{st.Size(), working}

	return working
}

// agyTurns are the transcripts agyWorking has read, by path.
var agyTurns = struct {
	sync.Mutex
	m map[string]agyTurn
}{m: map[string]agyTurn{}}

type agyTurn struct {
	size    int64
	working bool
}
