package daemon

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/xseman/pando/internal/proto"
)

// A claude job is a CronCreate call claude answered with an id: not one it
// refused or one that fires once, not after its CronDelete or its seven
// days. A self-paced loop is listed until a while past its wake-up, its
// stop, or the process that armed it.
func TestClaudeSchedule(t *testing.T) {
	log := filepath.Join(t.TempDir(), "conv.jsonl")
	at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	now := at.Add(time.Hour)

	create := func(call, cron string) string {
		return `{"timestamp":"2026-03-01T12:00:00Z","message":{"content":[{"type":"tool_use","id":"` + call + `","name":"CronCreate","input":{"cron":"` + cron + `","prompt":"check the build"}}]}}` + "\n"
	}
	answer := func(call, result string) string {
		return `{"timestamp":"2026-03-01T12:00:01Z","toolUseResult":` + result + `,"message":{"content":[{"type":"tool_result","tool_use_id":"` + call + `"}]}}` + "\n"
	}
	got := func(since, now time.Time) []proto.Schedule {
		return firing(scanSchedules(log, claudeSchedule), since, now)
	}

	mustWrite(t, log, `{"message":{"content":"a user who writes \"name\":\"CronCreate\""}}`+"\n"+create("toolu_1", "*/5 * * * *"))

	if s := got(time.Time{}, now); s != nil {
		t.Fatalf("a call not answered yet: %v", s)
	}

	appendTo(t, log, answer("toolu_1", `{"id":"aaaa1111","recurring":true,"durable":false}`)+
		create("toolu_2", "0 9 * * *")+answer("toolu_2", `"Error: invalid cron"`)+
		create("toolu_3", "30 14 2 3 *")+answer("toolu_3", `{"id":"cccc3333","recurring":false}`)+
		create("toolu_4", "0 * * * *")+answer("toolu_4", `{"id":"dddd4444","recurring":true,"durable":true}`))

	every5, hourly := proto.Schedule{Every: "*/5 * * * *", Prompt: "check the build"}, proto.Schedule{Every: "0 * * * *", Prompt: "check the build"}
	if s := got(time.Time{}, now); !slices.Equal(s, []proto.Schedule{every5, hourly}) {
		t.Fatalf("the two claude took: %v", s)
	}

	if s := got(now, now); !slices.Equal(s, []proto.Schedule{every5, hourly}) {
		t.Fatalf("a resume takes them up again: %v", s)
	}

	if s := got(time.Time{}, at.Add(scheduleExpiry+time.Hour)); s != nil {
		t.Fatalf("past seven days: %v", s)
	}

	appendTo(t, log, `{"message":{"content":[{"type":"tool_use","id":"toolu_5","name":"CronDelete","input":{"id":"aaaa1111"}}]}}`+"\n"+
		`{"timestamp":"2026-03-01T12:30:00Z","message":{"content":[{"type":"tool_use","id":"toolu_6","name":"ScheduleWakeup","input":{"delaySeconds":1200,"prompt":"watch the deploy"}}]}}`+"\n")

	loop := proto.Schedule{Every: selfPaced, Prompt: "watch the deploy"}
	if s := got(time.Time{}, now); !slices.Equal(s, []proto.Schedule{hourly, loop}) {
		t.Fatalf("one deleted, a loop armed: %v", s)
	}

	if s := got(time.Time{}, now.Add(time.Hour)); !slices.Equal(s, []proto.Schedule{hourly}) {
		t.Fatalf("a wake-up long past: %v", s)
	}

	if s := got(now, now); !slices.Equal(s, []proto.Schedule{hourly}) {
		t.Fatalf("a resume drops the loop: %v", s)
	}

	appendTo(t, log, `{"message":{"content":[{"type":"tool_use","id":"toolu_7","name":"ScheduleWakeup","input":{"stop":true}}]}}`+"\n")

	if s := got(time.Time{}, now); !slices.Equal(s, []proto.Schedule{hourly}) {
		t.Fatalf("the loop stopped: %v", s)
	}
}

// A copilot job is a recurring or self-paced schedule its events registered
// and did not cancel; an /after fires once and is none. A self-paced loop is
// listed until a while past the run it last armed.
func TestCopilotSchedules(t *testing.T) {
	dir := t.TempDir()
	files := []string{filepath.Join(dir, "inuse.42.hold")}
	events := filepath.Join(dir, "events.jsonl")

	mustWrite(t, events, `{"type":"session.schedule_created","data":{"id":1,"intervalMs":300000,"prompt":"run the tests","recurring":true}}`+"\n"+
		`{"type":"session.schedule_created","data":{"id":2,"at":1790000000000,"prompt":"once"}}`+"\n"+
		`{"type":"session.schedule_created","data":{"id":3,"cron":"0 9 * * 1-5","tz":"UTC","prompt":"expanded","displayPrompt":"/standup"}}`+"\n"+
		`{"type":"session.schedule_created","timestamp":"2026-11-18T09:00:00.000Z","data":{"id":4,"prompt":"watch the deploy","selfPaced":true}}`+"\n"+
		`{"type":"session.schedule_created","data":{"id":1,"intervalMs":5400000,"prompt":"run the tests","recurring":true}}`+"\n")

	made := time.Date(2026, 11, 18, 9, 1, 0, 0, time.UTC)

	want := []proto.Schedule{{Every: "0 9 * * 1-5", Prompt: "/standup"}, {Every: selfPaced, Prompt: "watch the deploy"}, {Every: "1h30m", Prompt: "run the tests"}}
	if got := copilotSchedules(files, made); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	appendTo(t, events, `{"type":"session.schedule_cancelled","data":{"id":3}}`+"\n")

	if got := copilotSchedules(files, made); !slices.Equal(got, want[1:]) {
		t.Fatalf("one cancelled: %v", got)
	}

	armed := time.UnixMilli(1795003600000)

	appendTo(t, events, `{"type":"session.schedule_rearmed","data":{"id":4,"nextRunAt":1795003600000}}`+"\n")

	if got := copilotSchedules(files, armed); !slices.Equal(got, want[1:]) {
		t.Fatalf("a loop armed: %v", got)
	}

	if got := copilotSchedules(files, armed.Add(time.Hour)); !slices.Equal(got, want[2:]) {
		t.Fatalf("a run long past, not armed again: %v", got)
	}

	if got := interval(300000); got != "5m" {
		t.Fatalf("five minutes: %q", got)
	}
}

// A grok job is a task the session grok holds open says it created: changed
// by a second word of it, gone with the word that it was deleted or with the
// process that made it.
func TestGrokSchedules(t *testing.T) {
	dir := t.TempDir()
	files := []string{filepath.Join(dir, "events.jsonl")}
	updates := filepath.Join(dir, "updates.jsonl")
	now := time.Unix(1793178900, 0)

	update := func(at, body string) string {
		return `{"timestamp":` + at + `,"method":"_x.ai/session/update","params":{"sessionId":"S1","update":{` + body + `}}}` + "\n"
	}

	mustWrite(t, filepath.Join(dir, "summary.json"), `{"session_kind":"main"}`)
	mustWrite(t, updates, update("1793178809", `"sessionUpdate":"tool_call","toolCallId":"call-1","rawInput":{"interval":"10m","prompt":"check CI"},"_meta":{"x.ai/tool":{"name":"scheduler_create"}}`)+
		update("1793178809", `"sessionUpdate":"scheduled_task_created","task_id":"a1b2c3","prompt":"check CI","human_schedule":"every 10 minutes","next_fire_at":"2026-10-28T10:10:00Z"`)+
		update("1793178809", `"sessionUpdate":"scheduled_task_created","task_id":"d4e5f6","prompt":"summarize issues","human_schedule":"every 1 hour"`)+
		update("1793178809", `"sessionUpdate":"scheduled_task_fired","task_id":"a1b2c3","prompt":"check CI","human_schedule":"every 10 minutes"`))

	want := []proto.Schedule{{Every: "10 minutes", Prompt: "check CI"}, {Every: "1 hour", Prompt: "summarize issues"}}
	if got := grokSchedules(files, time.Time{}, now); !slices.Equal(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}

	if got := grokSchedules(files, now, now); got != nil {
		t.Fatalf("a process started since, none taken up: %v", got)
	}

	appendTo(t, updates, update("1793178900", `"sessionUpdate":"scheduled_task_created","task_id":"d4e5f6","prompt":"summarize issues","human_schedule":"every 2 hours"`)+
		update("1793178900", `"sessionUpdate":"scheduled_task_deleted","task_id":"a1b2c3","reason":"expired"`))

	want = []proto.Schedule{{Every: "2 hours", Prompt: "summarize issues"}}
	if got := grokSchedules(files, now, now); !slices.Equal(got, want) {
		t.Fatalf("one changed, one deleted: got %v, want %v", got, want)
	}
}

// An agy job is a schedule call with a cron expression in the conversation
// agy holds open that was answered with a task id, until a manage_task kills
// that task or a message says it finished; a one-shot timer is none.
func TestAgySchedules(t *testing.T) {
	home := t.TempDir()
	files := []string{filepath.Join(home, "presence", "conv.lock")}
	transcript := filepath.Join(home, "brain", "conv", ".system_generated", "logs", "transcript.jsonl")
	now := time.Date(2026, 3, 1, 13, 0, 0, 0, time.UTC)

	schedule := func(step, cron, prompt string) string {
		return `{"step_index":` + step + `,"type":"PLANNER_RESPONSE","created_at":"2026-03-01T12:00:00Z","tool_calls":[{"name":"schedule","args":{"CronExpression":"\"` + cron + `\"","MaxIterations":"3","Prompt":"\"` + prompt + `\""}}]}` + "\n"
	}
	running := func(step, task string) string {
		return `{"step_index":` + step + `,"type":"GENERIC","status":"RUNNING","content":"Tool is running as a background task with task id: ` + task + `\nTask logs are available"}` + "\n"
	}
	kill := func(step, task string) string {
		return `{"step_index":` + step + `,"type":"PLANNER_RESPONSE","tool_calls":[{"name":"manage_task","args":{"Action":"\"kill\"","TaskId":"\"` + task + `\""}}]}` + "\n"
	}

	mustWrite(t, transcript, `{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-03-01T12:00:00Z","tool_calls":[`+
		`{"name":"run_command","args":{"CommandLine":"\"ls\""}},`+
		`{"name":"schedule","args":{"CronExpression":"\"*/10 * * * *\"","Prompt":"\"Check the workers\""}},`+
		`{"name":"schedule","args":{"DurationSeconds":"300","Prompt":"\"Remind the user\""}}]}`+"\n"+
		`{"step_index":2,"type":"GENERIC","content":"file.go"}`+"\n")

	if got := agySchedules(files, time.Time{}, now); got != nil {
		t.Fatalf("a call not answered yet: %v", got)
	}

	appendTo(t, transcript, running("3", "conv/task-3")+running("4", "conv/task-4")+
		schedule("5", "* * * * * *", "Refused")+`{"step_index":6,"type":"GENERIC","content":"invalid CronExpression: expected 5 fields"}`+"\n"+
		schedule("7", "0 * * * *", "Sweep the queue")+running("18", "conv/task-18"))

	workers, sweep := proto.Schedule{Every: "*/10 * * * *", Prompt: "Check the workers"}, proto.Schedule{Every: "0 * * * *", Prompt: "Sweep the queue"}
	if got := agySchedules(files, time.Time{}, now); !slices.Equal(got, []proto.Schedule{workers, sweep}) {
		t.Fatalf("the two agy took: %v", got)
	}

	if got := agySchedules(files, now, now); got != nil {
		t.Fatalf("a process started since: %v", got)
	}

	appendTo(t, transcript, kill("19", "conv/task-4")+`{"step_index":20,"type":"GENERIC","content":"killed"}`+"\n"+kill("21", "conv/task-1"))

	if got := agySchedules(files, time.Time{}, now); !slices.Equal(got, []proto.Schedule{workers, sweep}) {
		t.Fatalf("other tasks killed: %v", got)
	}

	appendTo(t, transcript, `{"step_index":22,"type":"GENERIC","content":"no such task"}`+"\n"+
		`{"step_index":23,"type":"SYSTEM_MESSAGE","content":"<SYSTEM_MESSAGE>\n[Message] sender=conv/task-18 content=Task id \"conv/task-18\" finished with result:\nCron schedule completed after 3 iterations.\n</SYSTEM_MESSAGE>"}`+"\n")

	if got := agySchedules(files, time.Time{}, now); !slices.Equal(got, []proto.Schedule{workers}) {
		t.Fatalf("one ran out: %v", got)
	}

	appendTo(t, transcript, kill("24", "conv/task-3"))

	if got := agySchedules(files, time.Time{}, now); got != nil {
		t.Fatalf("killed: %v", got)
	}
}

// startedAt is the time /proc gives a process: this one's lies behind now.
func TestStartedAt(t *testing.T) {
	if _, err := os.Stat("/proc/self/stat"); err != nil {
		t.Skip("no /proc")
	}

	at := startedAt(os.Getpid())
	if at.IsZero() || at.After(time.Now()) || time.Since(at) > time.Hour {
		t.Fatalf("this process started at %v", at)
	}
}
