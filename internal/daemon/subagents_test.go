package daemon

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// appendTo adds s to the end of file path.
func appendTo(t *testing.T, path, s string) {
	t.Helper()

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := f.WriteString(s); err != nil {
		t.Fatal(err)
	}

	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

// A codex subagent is an open rollout whose session_meta says an agent
// spawned it; it works from its own task_started to task_complete, after
// the copy of its parent's history it starts with.
func TestCodexSubagents(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "rollout-2026-10-28T09-57-39-root.jsonl")
	kid := filepath.Join(dir, "rollout-2026-10-28T09-57-43-kid.jsonl")
	files := []string{root, kid, filepath.Join(dir, "state_5.sqlite")}

	mustWrite(t, root, `{"timestamp":"2026-10-28T08:57:39.849Z","type":"session_meta","payload":{"id":"root","timestamp":"2026-10-28T08:57:39.849Z","source":"exec"}}`+"\n"+
		`{"timestamp":"2026-10-28T08:57:40Z","type":"event_msg","payload":{"type":"task_started"}}`+"\n")
	mustWrite(t, kid, `{"timestamp":"2026-10-28T08:57:43.772Z","type":"session_meta","payload":{"id":"kid","timestamp":"2026-10-28T08:57:43.772Z",`+
		`"agent_nickname":"Harvey","agent_path":"/root/find_callers","source":{"subagent":{"thread_spawn":{"parent_thread_id":"root","depth":1}}}`)

	if got := codexSubagents(files); got != nil {
		t.Fatalf("a first line half written: %v", got)
	}

	appendTo(t, kid, "}}\n"+
		`{"timestamp":"2026-10-28T08:57:43Z","type":"event_msg","payload":{"type":"task_started"}}`+"\n"+
		`{"timestamp":"2026-10-28T08:57:43Z","type":"event_msg","payload":{"type":"turn_aborted"}}`+"\n")

	if got := codexSubagents(files); got != nil {
		t.Fatalf("the parent's turn it copied is over: %v", got)
	}

	if model, effort := codexModel(files); model != "" || effort != "" {
		t.Fatalf("no turn of the conversation's yet: %q %q", model, effort)
	}
	// Each turn names what it runs on, the effort only off the model's own.
	appendTo(t, root, `{"type":"turn_context","payload":{"model":"gpt-6-luna","collaboration_mode":{"settings":{"model":"gpt-6-luna","reasoning_effort":null}}}}`+"\n")
	appendTo(t, kid, `{"timestamp":"2026-10-28T08:57:44Z","type":"event_msg","payload":{"type":"task_started"}}`+"\n"+
		`{"type":"turn_context","payload":{"model":"gpt-6-luna","effort":"low"}}`+"\n"+
		`{"type":"turn_context","payload":{"model":"gpt-6-sol","effort":"medium"}}`+"\n"+
		`{"timestamp":"2026-10-28T08:57:46Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec"}}`+"\n")

	want := []proto.Subagent{{
		Type: "Harvey", Description: "find callers", Started: time.Date(2026, 10, 28, 8, 57, 43, 772e6, time.UTC),
		Model: "gpt-6-sol", Effort: "medium",
	}}
	if got := codexSubagents(files); !slices.Equal(got, want) {
		t.Fatalf("its own turn runs: %v", got)
	}

	if model, effort := codexModel(files); model != "gpt-6-luna" || effort != "" {
		t.Fatalf("the conversation's last turn: %q %q", model, effort)
	}

	appendTo(t, kid, `{"timestamp":"2026-10-28T08:58:48Z","type":"event_msg","payload":{"type":"task_complete","last_agent_message":"done"}}`+"\n")

	if got := codexSubagents(files); got != nil {
		t.Fatalf("done: %v", got)
	}
}

// An agy subagent of the conversation whose presence lock the process holds
// is listed while alive and its turn goes on: not after a final reply, nor
// once killed.
func TestAgySubagents(t *testing.T) {
	home := t.TempDir()
	lock := filepath.Join(home, "presence", "conv.lock")
	files := []string{lock, filepath.Join(home, "conversations", "conv.db")}
	meta := filepath.Join(home, "brain", "conv", ".system_generated", "subagents", "kid.json")
	transcript := filepath.Join(home, "brain", "kid", ".system_generated", "logs", "transcript.jsonl")

	mustWrite(t, lock, "")

	if got := agySubagents(files); got != nil {
		t.Fatalf("no subagent yet: %v", got)
	}

	mustWrite(t, meta, `{"conversationId":"kid", "subagentDescriptor":{"typeName":"self", "role":"find callers"}, "state":"SUBAGENT_STATE_ALIVE"}`)
	mustWrite(t, transcript, `{"step_index":0,"source":"SYSTEM","type":"SYSTEM_MESSAGE","status":"DONE"}`+"\n"+
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","tool_calls":[{"name":"run_command"}]}`+"\n")

	at := time.Now().Truncate(time.Second)
	if err := os.Chtimes(meta, at, at); err != nil {
		t.Fatal(err)
	}

	want := []proto.Subagent{{Type: "subagent", Description: "find callers", Started: at}}
	if got := agySubagents(files); !slices.Equal(got, want) {
		t.Fatalf("a tool call on the way: %v", got)
	}

	appendTo(t, transcript, `{"step_index":2,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","content":"done"}`+"\n")

	if got := agySubagents(files); got != nil {
		t.Fatalf("a final reply ends its turn: %v", got)
	}

	appendTo(t, transcript, `{"step_index":3,"source":"SYSTEM","type":"SYSTEM_MESSAGE","status":"DONE"}`+"\n")

	if got := agySubagents(files); len(got) != 1 {
		t.Fatalf("woken again: %v", got)
	}

	mustWrite(t, meta, `{"conversationId":"kid", "subagentDescriptor":{"typeName":"self", "role":"find callers"}, "state":"SUBAGENT_STATE_KILLED"}`)

	if got := agySubagents(files); got != nil {
		t.Fatalf("killed: %v", got)
	}
}

// A grok subagent is listed while its meta.json says running and its own
// session is open in the process: a crash's "running" leftover is not.
func TestGrokSubagents(t *testing.T) {
	cwd := filepath.Join(t.TempDir(), "%2Fhome%2Fme%2Fapp")
	events := func(id string) string { return filepath.Join(cwd, id, "events.jsonl") }
	meta := filepath.Join(cwd, "parent", "subagents", "kid", "meta.json")
	running := `{"child_session_id":"kid","subagent_type":"explore","description":"find callers","status":"running","started_at":"2026-10-28T09:13:29.517734987Z"}`

	mustWrite(t, meta, running)

	if got := grokSubagents([]string{events("parent")}); got != nil {
		t.Fatalf("its session not open, a crash's leftover: %v", got)
	}

	files := []string{events("parent"), events("kid"), filepath.Join(cwd, "kid", "terminal", "call-1")}
	want := []proto.Subagent{{Type: "explore", Description: "find callers", Started: time.Date(2026, 10, 28, 9, 13, 29, 517734987, time.UTC)}}

	if got := grokSubagents(files); !slices.Equal(got, want) {
		t.Fatalf("running: %v", got)
	}

	// Each session's summary names what it runs on: the subagent's its own,
	// the process's the one that is no subagent.
	if model, effort := grokModel(files); model != "" || effort != "" {
		t.Fatalf("no summary yet: %q %q", model, effort)
	}

	mustWrite(t, filepath.Join(cwd, "kid", "summary.json"), `{"current_model_id":"grok-4.6","session_kind":"subagent","reasoning_effort":"low"}`)
	mustWrite(t, filepath.Join(cwd, "parent", "summary.json"), `{"current_model_id":"grok-4.7","session_kind":"headless","reasoning_effort":"high"}`)

	want[0].Model, want[0].Effort = "grok-4.6", "low"
	if got := grokSubagents(files); !slices.Equal(got, want) {
		t.Fatalf("on its own model: %v", got)
	}

	for _, fs := range [][]string{files, {events("kid"), events("parent")}} { // whichever /proc lists first
		if model, effort := grokModel(fs); model != "grok-4.7" || effort != "high" {
			t.Fatalf("the session's own, not its subagent's: %q %q", model, effort)
		}
	}

	mustWrite(t, meta, strings.Replace(running, `"running"`, `"completed"`, 1))

	if got := grokSubagents(files); got != nil {
		t.Fatalf("completed: %v", got)
	}
}

// Copilot's session, the one whose inuse hold the process has open, runs on
// the model picked last, or the one a reply of its own said since: not a
// subagent's.
func TestCopilotModel(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "session-state", "s1")
	events := filepath.Join(dir, "events.jsonl")
	files := []string{filepath.Join(dir, "inuse.42.hold")}
	model := func() string {
		model, effort := copilotModel(files)

		return model + " " + effort
	}

	if got := model(); got != " " {
		t.Fatalf("no events yet: %q", got)
	}

	mustWrite(t, events, `{"type":"session.start","data":{"selectedModel":"auto"}}`+"\n"+
		`{"type":"session.model_change","data":{"newModel":"auto","reasoningEffort":"high"}}`+"\n")

	if got := model(); got != "auto high" {
		t.Fatalf("picked: %q", got)
	}

	appendTo(t, events, `{"type":"session.auto_mode_resolved","data":{"chosenModel":"gpt-6-luna"}}`+"\n"+
		`{"type":"assistant.message","data":{"parentToolCallId":"call_1","model":"claude-haiku-4.5"}}`+"\n"+
		`{"type":"assistant.message","data":{"content":"hi"}}`+"\n"+
		`{"type":"assistant.mess`)

	if got := model(); got != "gpt-6-luna high" {
		t.Fatalf("copilot's choice, past a subagent's reply: %q", got)
	}

	appendTo(t, events, `age","data":{"model":"gpt-5.5"}}`+"\n")

	if got := model(); got != "gpt-5.5 high" {
		t.Fatalf("its own reply, once whole: %q", got)
	}

	appendTo(t, events, `{"type":"session.model_change","data":{"previousModel":"gpt-5.5","newModel":"claude-opus-4.8"}}`+"\n")

	if got := model(); got != "claude-opus-4.8 " {
		t.Fatalf("picked again, on the model's own effort: %q", got)
	}
}

// Agy names the model in the note its transcript carries of a changed Model
// Selection: the last one stands, its effort the part in brackets.
func TestAgyModel(t *testing.T) {
	home := t.TempDir()
	files := []string{filepath.Join(home, "presence", "c1.lock")}
	transcript := filepath.Join(home, "brain", "c1", ".system_generated", "logs", "transcript.jsonl")
	note := func(from, to string) string {
		return `{"type":"USER_INPUT","content":"<USER_SETTINGS_CHANGE>\nThe user changed setting ` + "`Model Selection`" + ` from ` + from + ` to ` + to + `. No need to comment on this change.\n"}` + "\n"
	}

	mustWrite(t, transcript, `{"type":"USER_INPUT","content":"hello"}`+"\n")

	if model, effort := agyModel(files); model != "" || effort != "" {
		t.Fatalf("no note yet: %q %q", model, effort)
	}

	appendTo(t, transcript, note("None", "Gemini 3.8 Flash (Medium)")+`{"type":"PLANNER_RESPONSE"}`+"\n")

	if model, effort := agyModel(files); model != "Gemini 3.8 Flash" || effort != "medium" {
		t.Fatalf("the model it started on: %q %q", model, effort)
	}

	appendTo(t, transcript, note("Gemini 3.8 Flash (Medium)", "Claude Opus 5.5"))

	if model, effort := agyModel(files); model != "Claude Opus 5.5" || effort != "" {
		t.Fatalf("changed, to one without an effort: %q %q", model, effort)
	}
}

// Gemini's session is the one written last for the project its process
// runs in, by the name projects.json gives the directory, else its hash.
func TestGeminiModel(t *testing.T) {
	home, dir := t.TempDir(), "/home/me/app"
	reply := func(model string) string {
		return `{"id":"1","type":"gemini","content":"hi","model":"` + model + `"}` + "\n"
	}

	if model, _ := geminiModel(home, dir); model != "" {
		t.Fatalf("no session yet: %q", model)
	}

	chats := filepath.Join(home, "tmp", "app", "chats")
	mustWrite(t, filepath.Join(home, "projects.json"), `{"projects":{"/home/me/app":"app","/home/me/other":"other"}}`)
	mustWrite(t, filepath.Join(chats, "session-2026-11-01T10-00-aaaaaaaa.jsonl"), `{"sessionId":"a","kind":"main"}`+"\n"+reply("gemini-3.5-pro"))
	mustWrite(t, filepath.Join(home, "tmp", "other", "chats", "session-2026-11-02T10-00-cccccccc.jsonl"), reply("gemini-other"))

	old := time.Now().Add(-time.Hour)
	if err := os.Chtimes(filepath.Join(chats, "session-2026-11-01T10-00-aaaaaaaa.jsonl"), old, old); err != nil {
		t.Fatal(err)
	}

	if model, effort := geminiModel(home, dir); model != "gemini-3.5-pro" || effort != "" {
		t.Fatalf("the project's session: %q %q", model, effort)
	}

	latest := filepath.Join(chats, "session-2026-11-03T10-00-bbbbbbbb.jsonl")
	mustWrite(t, latest, `{"sessionId":"b","kind":"main"}`+"\n"+`{"type":"user","content":"\"type\":\"gemini\""}`+"\n")

	if model, _ := geminiModel(home, dir); model != "" {
		t.Fatalf("the one written last has no reply yet: %q", model)
	}

	appendTo(t, latest, reply("gemini-3.8-flash"))

	if model, _ := geminiModel(home, dir); model != "gemini-3.8-flash" {
		t.Fatalf("its reply: %q", model)
	}
	// Before gemini named projects, the directory's hash did.
	sum := sha256.Sum256([]byte("/home/me/old"))
	mustWrite(t, filepath.Join(home, "tmp", hex.EncodeToString(sum[:]), "chats", "session-2026-01-01T10-00-dddddddd.jsonl"), reply("gemini-2.5-pro"))

	if model, _ := geminiModel(home, "/home/me/old"); model != "gemini-2.5-pro" {
		t.Fatalf("by the hash: %q", model)
	}
}
