package daemon

import (
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

	appendTo(t, kid, `{"timestamp":"2026-10-28T08:57:44Z","type":"event_msg","payload":{"type":"task_started"}}`+"\n"+
		`{"timestamp":"2026-10-28T08:57:46Z","type":"response_item","payload":{"type":"custom_tool_call","name":"exec"}}`+"\n")

	want := []proto.Subagent{{Type: "Harvey", Description: "find callers", Started: time.Date(2026, 10, 28, 8, 57, 43, 772e6, time.UTC)}}
	if got := codexSubagents(files); !slices.Equal(got, want) {
		t.Fatalf("its own turn runs: %v", got)
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

	mustWrite(t, meta, strings.Replace(running, `"running"`, `"completed"`, 1))

	if got := grokSubagents(files); got != nil {
		t.Fatalf("completed: %v", got)
	}
}
