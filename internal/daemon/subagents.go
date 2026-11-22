package daemon

import (
	"bufio"
	"bytes"
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// subagentReaders read what an agent's process runs now, by program: its
// subagents, oldest first, the model and effort its own last turn ran on,
// and the recurring jobs it set up for itself (schedules.go). pid is the
// agent's process, c the conversation remember found for it. Copilot and
// gemini list no subagents, codex and gemini no jobs; opencode reports
// nothing.
var subagentReaders = map[string]func(pid int, c *proto.Conversation, now time.Time) agentRuns{
	"claude": func(pid int, c *proto.Conversation, now time.Time) agentRuns {
		if c == nil {
			return agentRuns{}
		}

		subagents := claudeSubagents(*c, now)
		model, effort := claudeModel(*c)

		return agentRuns{subagents, model, effort, claudeSchedules(*c, startedAt(pid), now)}
	},
	"codex": func(pid int, _ *proto.Conversation, _ time.Time) agentRuns {
		files := openFiles(pid)
		subagents := codexSubagents(files)
		model, effort := codexModel(files)

		return agentRuns{subagents, model, effort, nil}
	},
	"agy": func(pid int, _ *proto.Conversation, now time.Time) agentRuns {
		files := openFiles(pid)
		model, effort := agyModel(files)

		return agentRuns{agySubagents(files), model, effort, agySchedules(files, startedAt(pid), now)}
	},
	"gemini": func(pid int, _ *proto.Conversation, _ time.Time) agentRuns {
		dir, _ := os.Readlink("/proc/" + strconv.Itoa(pid) + "/cwd") // gone, or not on Linux: no session found

		home := envOf(pid, "GEMINI_CLI_HOME")
		if home == "" {
			home, _ = os.UserHomeDir()
		}

		model, effort := geminiModel(filepath.Join(home, ".gemini"), dir)

		return agentRuns{nil, model, effort, nil}
	},
	"copilot": func(pid int, _ *proto.Conversation, now time.Time) agentRuns {
		files := openFiles(pid)
		model, effort := copilotModel(files)

		return agentRuns{nil, model, effort, copilotSchedules(files, now)}
	},
	"grok": func(pid int, _ *proto.Conversation, now time.Time) agentRuns {
		files := openFiles(pid)
		model, effort := grokModel(files)

		return agentRuns{grokSubagents(files), model, effort, grokSchedules(files, startedAt(pid), now)}
	},
}

// agentRuns is what a subagentReaders row read.
type agentRuns struct {
	subagents     []proto.Subagent
	model, effort string
	schedules     []proto.Schedule
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

		model, effort := claudeRuns(filepath.Join(sc.dir, name+".jsonl"))
		out = append(out, proto.Subagent{Type: v.Type, Description: v.Description, Started: meta.ModTime(), Model: model, Effort: effort})
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

// claudeModel is the model and effort conversation c's last reply ran on,
// "" until claudeSubagents has found its transcript.
func claudeModel(c proto.Conversation) (model, effort string) {
	subs.Lock()
	sc := subs.m[c.ID]
	subs.Unlock()

	if sc == nil {
		return "", ""
	}

	return claudeRuns(filepath.Dir(sc.dir) + ".jsonl")
}

// claudeTail is how much of a transcript's end claudeRuns reads for its last
// reply.
// ponytail: a transcript that ends in more than this without a whole reply,
// one huge tool result, keeps what the reply before it said.
const claudeTail = 256 << 10

// claudeRuns is the model and effort of the last reply in claude transcript
// path, the conversation's or a subagent's: each assistant line carries
// message.model and effort. "" before the first; a transcript is read again
// only once it grew.
func claudeRuns(path string) (model, effort string) {
	claudeRun.Lock()
	defer claudeRun.Unlock()

	st, err := os.Stat(path)
	if err != nil {
		return "", ""
	}

	r := claudeRun.m[path]
	if r.size == st.Size() {
		return r.model, r.effort
	}

	f, err := os.Open(path)
	if err != nil {
		return r.model, r.effort
	}
	defer func() { _ = f.Close() }() // read only: nothing to lose

	off := max(st.Size()-claudeTail, 0)
	b := make([]byte, st.Size()-off)
	n, _ := f.ReadAt(b, off)

	lines := bytes.Split(b[:n], []byte{'\n'})
	for _, line := range slices.Backward(lines) {
		if !bytes.Contains(line, []byte(`"effort":"`)) { // the rest hold whole tool output
			continue
		}

		var v struct {
			Message struct {
				Model string `json:"model"`
			} `json:"message"`
			Effort string `json:"effort"`
		}

		if json.Unmarshal(line, &v) == nil && v.Message.Model != "" && v.Effort != "" {
			r.model, r.effort = v.Message.Model, v.Effort
			break
		}
	}

	r.size = st.Size()
	claudeRun.m[path] = r

	return r.model, r.effort
}

// claudeRun are the transcripts claudeRuns has read, by path.
var claudeRun = struct {
	sync.Mutex
	m map[string]claudeRan
}{m: map[string]claudeRan{}}

type claudeRan struct {
	size          int64
	model, effort string
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
// to its task_complete or turn_aborted, on the model and effort its last
// turn_context names.
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

		t.read(p)

		if t.sub.Type == "" { // the conversation itself: read for codexModel
			continue
		}

		if t.running {
			out = append(out, t.sub)
		}
	}

	return byStart(out)
}

// codexModel is the model and effort of the conversation among the rollouts
// open in files, as codexSubagents last read it: its last turn's. A turn on
// the model's own effort names none.
func codexModel(files []string) (model, effort string) {
	codexThreads.Lock()
	defer codexThreads.Unlock()

	for _, p := range files {
		if t := codexThreads.m[p]; t != nil && t.sub.Type == "" {
			return t.sub.Model, t.sub.Effort
		}
	}

	return "", ""
}

// codexThreads are the rollouts codexSubagents has read, by path.
var codexThreads = struct {
	sync.Mutex
	m map[string]*codexThread
}{m: map[string]*codexThread{}}

// codexThread is a rollout as codexSubagents knows it: the subagent it
// writes (no Type for one that is none, the conversation: its model and
// effort only), how far it was read, and whether its turn was going at that
// point.
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
// turn's last task event and turn_context; a file that shrank is read again
// from the start.
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
		if !bytes.Contains(line, []byte(`"type":"event_msg"`)) && !bytes.Contains(line, []byte(`"type":"turn_context"`)) { // the rest hold whole tool output
			continue
		}

		var v struct {
			Type    string `json:"type"`
			Payload struct {
				Type   string `json:"type"`
				Model  string `json:"model"`
				Effort string `json:"effort"`
			} `json:"payload"`
		}

		if json.Unmarshal(line, &v) != nil {
			continue
		}

		switch {
		case v.Type == "turn_context":
			t.sub.Model, t.sub.Effort = v.Payload.Model, v.Payload.Effort
		case v.Payload.Type == "task_started":
			t.running = true
		case v.Payload.Type == "task_complete", v.Payload.Type == "turn_aborted":
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
// one a crash left "running" is not open, so it is left out. Its session's
// summary.json names the model and effort it runs on.
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

			model, effort, _ := grokSummary(open[v.Child])
			out = append(out, proto.Subagent{Type: v.Type, Description: v.Description, Started: v.Started, Model: model, Effort: effort})
		}
	}

	return byStart(out)
}

// grokModel is the model and effort of the session a grok process holds
// open in files that is no subagent's.
func grokModel(files []string) (model, effort string) {
	for _, p := range files {
		if filepath.Base(p) != "events.jsonl" {
			continue
		}

		if model, effort, kind := grokSummary(filepath.Dir(p)); kind != "subagent" && model != "" {
			return model, effort
		}
	}

	return "", ""
}

// grokSummary reads a grok session's summary.json in dir: the model and
// effort it runs on, and its kind, "subagent" for one a session spawned.
// ponytail: one caught half written reads as none until the next tick.
func grokSummary(dir string) (model, effort, kind string) {
	b, err := os.ReadFile(filepath.Join(dir, "summary.json"))

	var v struct {
		Model  string `json:"current_model_id"`
		Effort string `json:"reasoning_effort"`
		Kind   string `json:"session_kind"`
	}

	if err != nil || json.Unmarshal(b, &v) != nil {
		return "", "", ""
	}

	return v.Model, v.Effort, v.Kind
}

// modelScan is how far a log was read for the model and effort its agent
// runs on, and the last it named.
type modelScan struct {
	off           int64
	model, effort string
}

// modelScans are the logs scanModel has read, by path.
var modelScans = struct {
	sync.Mutex
	m map[string]*modelScan
}{m: map[string]*modelScan{}}

// scanModel is the model and effort the log at path last named: take reads
// them off each whole line appended since the last scan. A log that shrank
// is read again from the start.
func scanModel(path string, take func(s *modelScan, line []byte)) (model, effort string) {
	modelScans.Lock()
	defer modelScans.Unlock()

	s := modelScans.m[path]
	if s == nil {
		s = &modelScan{}
		modelScans.m[path] = s
	}

	f, err := os.Open(path)
	if err != nil {
		return s.model, s.effort
	}
	defer func() { _ = f.Close() }() // read only: nothing to lose

	st, err := f.Stat()
	if err != nil || st.Size() == s.off {
		return s.model, s.effort
	}

	if st.Size() < s.off {
		*s = modelScan{}
	}

	b := make([]byte, st.Size()-s.off)
	n, _ := f.ReadAt(b, s.off)

	end := bytes.LastIndexByte(b[:n], '\n')
	if end < 0 { // a line still being written: it is read whole next time
		return s.model, s.effort
	}

	for line := range bytes.SplitSeq(b[:end], []byte{'\n'}) {
		take(s, line)
	}

	s.off += int64(end + 1)

	return s.model, s.effort
}

// copilotModel is the model and effort of the session a copilot process
// holds in files, by its inuse.<pid>.hold: the session's events.jsonl names
// the one picked (session.model_change, "auto" for copilot's choice), the
// one copilot chose (session.auto_mode_resolved) and the one each reply of
// its own ran on (assistant.message).
func copilotModel(files []string) (model, effort string) {
	for _, p := range files {
		if base := filepath.Base(p); !strings.HasPrefix(base, "inuse.") || filepath.Ext(base) != ".hold" {
			continue
		}

		return scanModel(filepath.Join(filepath.Dir(p), "events.jsonl"), func(s *modelScan, line []byte) {
			if !bytes.Contains(line, []byte(`"type":"session.`)) && !bytes.Contains(line, []byte(`"type":"assistant.message"`)) { // the rest hold whole tool output
				return
			}

			var v struct {
				Type string `json:"type"`
				Data struct {
					New    string `json:"newModel"`
					Effort string `json:"reasoningEffort"`
					Chosen string `json:"chosenModel"`
					Model  string `json:"model"`
					Parent string `json:"parentToolCallId"` // a subagent's reply
				} `json:"data"`
			}

			if json.Unmarshal(line, &v) != nil {
				return
			}

			switch {
			case v.Type == "session.model_change":
				s.model, s.effort = v.Data.New, v.Data.Effort
			case v.Type == "session.auto_mode_resolved" && v.Data.Chosen != "":
				s.model = v.Data.Chosen
			case v.Type == "assistant.message" && v.Data.Parent == "" && v.Data.Model != "":
				s.model = v.Data.Model
			}
		})
	}

	return "", ""
}

// geminiModel is the model of the gemini session written last for the
// project in dir, under config directory home: tmp/<project>/chats holds a
// session-*.jsonl each, whose replies ("type":"gemini") name their model.
// The project goes by the name projects.json gives dir, by the hash of dir
// before gemini kept that. Gemini records no effort.
// ponytail: gemini holds no file open, so two sessions in one directory
// both read as the one written last.
func geminiModel(home, dir string) (model, effort string) {
	if dir == "" {
		return "", ""
	}

	var reg struct {
		Projects map[string]string `json:"projects"`
	}

	b, _ := os.ReadFile(filepath.Join(home, "projects.json")) // none: the hash stands in
	_ = json.Unmarshal(b, &reg)

	sum := sha256.Sum256([]byte(dir))
	id := hex.EncodeToString(sum[:])

	if name := reg.Projects[dir]; name != "" {
		id = filepath.Base(name) // a name, never a path out of tmp
	}

	var (
		last string
		at   time.Time
	)

	chats := filepath.Join(home, "tmp", id, "chats")
	es, _ := os.ReadDir(chats) // none yet: it comes with the first message

	for _, e := range es {
		if !strings.HasPrefix(e.Name(), "session-") || filepath.Ext(e.Name()) != ".jsonl" {
			continue
		}

		if info, err := e.Info(); err == nil && info.ModTime().After(at) {
			last, at = e.Name(), info.ModTime()
		}
	}

	if last == "" {
		return "", ""
	}

	return scanModel(filepath.Join(chats, last), func(s *modelScan, line []byte) {
		if !bytes.Contains(line, []byte(`"type":"gemini"`)) {
			return
		}

		var v struct {
			Type  string `json:"type"`
			Model string `json:"model"`
		}

		if json.Unmarshal(line, &v) == nil && v.Type == "gemini" && v.Model != "" {
			s.model = v.Model
		}
	})
}

// agySetting is how agy tells its model of a changed Model Selection, in the
// user's next message: "… from None to Gemini 3.8 Flash (Medium). No need …".
var agySetting = regexp.MustCompile("`Model Selection` from .*? to (.+?)(?: \\((\\w+)\\))?\\. No need")

// agyModel is the model and effort of the conversation an agy process has
// open, the one whose presence/<id>.lock it holds in files.
// ponytail: agy keeps the selection in a protobuf, so it is read off the
// note in the transcript, by its wording; a subagent's has none.
func agyModel(files []string) (model, effort string) {
	for _, p := range files {
		if filepath.Base(filepath.Dir(p)) != "presence" || filepath.Ext(p) != ".lock" {
			continue
		}

		home, id := filepath.Dir(filepath.Dir(p)), strings.TrimSuffix(filepath.Base(p), ".lock")

		return scanModel(filepath.Join(home, "brain", id, ".system_generated", "logs", "transcript.jsonl"), func(s *modelScan, line []byte) {
			if ms := agySetting.FindAllSubmatch(line, -1); len(ms) > 0 {
				m := ms[len(ms)-1]
				s.model, s.effort = string(m[1]), strings.ToLower(string(m[2]))
			}
		})
	}

	return "", ""
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
