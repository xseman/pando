package daemon

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"io"
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

// The recurring jobs an agent set up for itself are read off the log its
// subagentReaders row already finds:
//
//	claude   CronCreate, CronDelete and ScheduleWakeup calls in the transcript
//	copilot  session.schedule_created, _cancelled and _rearmed in events.jsonl,
//	         as github.com/microsoft/copilot-sdk's session events schema names them
//	grok     scheduled_task_created and _deleted in updates.jsonl, as
//	         github.com/xai-org/grok-build writes them
//	agy      schedule and manage_task calls in the transcript
//
// Codex and gemini have none: codex's automations are the desktop app's.
// ponytail: agy's are read by its binary's strings and the lines of its
// other tools, a real job's unseen: a job whose answer names its id another
// way stays listed after its kill, until its process ends. Opencode's
// automations, a table of its database, are not read.

// scheduleExpiry is how long claude and grok keep a recurring job.
const scheduleExpiry = 7 * 24 * time.Hour

// wakeSlack is how long past its wake-up a self-paced loop still counts:
// the turn the wake-up starts sets the next one, and claude wakes a loop
// that set none some twenty minutes on.
// ponytail: a turn longer than this drops the loop off the list until then.
const wakeSlack = 25 * time.Minute

// selfPaced is the cadence of a job whose agent picks each delay itself.
const selfPaced = "self-paced"

// schedJob is one recurring job a log names.
type schedJob struct {
	call    string    // the tool call that made it
	id      string    // the id its agent gave it, where the log says
	text    []byte    // what the call was answered, else: a cancel names an id in it
	at      time.Time // when it was made
	until   time.Time // when it ends by itself; zero for never
	durable bool      // outlives its agent's process
	pending bool      // made, not answered yet
	s       proto.Schedule
}

// schedScan is how far a log was read for recurring jobs, and the ones not
// cancelled since.
type schedScan struct {
	off   int64
	jobs  []schedJob
	wake  schedJob // claude's self-paced loop: its last ScheduleWakeup
	calls []string // agy: the unanswered calls of its last reply, a schedule's by its job's call, "" for another's
}

// schedScans are the logs scanSchedules has read, by path.
// ponytail: a log's entry stays after its session is gone, a few hundred
// bytes each until the daemon restarts.
var schedScans = struct {
	sync.Mutex
	m map[string]*schedScan
}{m: map[string]*schedScan{}}

// scanSchedules are the jobs the log at path names that were not cancelled:
// take reads them off each whole line appended since the last scan. A log
// that shrank is read again from the start.
// ponytail: a transcript is read whole once, tens of megabytes on the tick
// that first sees a long conversation.
func scanSchedules(path string, take func(s *schedScan, line []byte)) []schedJob {
	schedScans.Lock()
	defer schedScans.Unlock()

	s := schedScans.m[path]
	if s == nil {
		s = &schedScan{}
		schedScans.m[path] = s
	}

	if st, err := os.Stat(path); err == nil && st.Size() < s.off {
		*s = schedScan{}
	}

	if f, err := os.Open(path); err == nil {
		defer func() { _ = f.Close() }() // read only: nothing to lose

		_, _ = f.Seek(s.off, io.SeekStart) // a failed seek reads lines take has seen: a job twice, until it is cancelled

		for r := bufio.NewReader(f); ; {
			line, err := r.ReadBytes('\n')
			if err != nil { // a line still being written: it is read whole next time
				break
			}

			s.off += int64(len(line))
			take(s, line)
		}
	}

	jobs := slices.Clone(s.jobs)
	if s.wake.s.Every != "" {
		jobs = append(jobs, s.wake)
	}

	return jobs
}

// firing are the jobs that still fire at now, oldest first: answered, not
// past their end, and made by the process that started at since unless they
// outlive it. A zero since, where /proc does not say, keeps them all.
func firing(jobs []schedJob, since, now time.Time) []proto.Schedule {
	var out []proto.Schedule

	for _, j := range jobs {
		gone := !j.durable && j.at.Before(since)
		over := !j.until.IsZero() && now.After(j.until)

		if !j.pending && !gone && !over {
			out = append(out, j.s)
		}
	}

	return out
}

// find is the job the agent calls id, -1 for none.
func (s *schedScan) find(id string) int {
	if id == "" {
		return -1
	}

	if i := slices.IndexFunc(s.jobs, func(j schedJob) bool { return j.id == id }); i >= 0 {
		return i
	}

	named := regexp.MustCompile(`\b` + regexp.QuoteMeta(id) + `\b`)

	return slices.IndexFunc(s.jobs, func(j schedJob) bool { return j.id == "" && named.Match(j.text) })
}

// cancel drops the job the agent calls id.
func (s *schedScan) cancel(id string) {
	if i := s.find(id); i >= 0 {
		s.jobs = slices.Delete(s.jobs, i, i+1)
	}
}

// startedAt is when process pid started, zero where /proc does not say.
func startedAt(pid int) time.Time {
	ticks, err := strconv.ParseInt(startTime(pid), 10, 64)
	if err != nil {
		return time.Time{}
	}

	b, _ := os.ReadFile("/proc/stat") // none: no btime below
	_, rest, _ := bytes.Cut(b, []byte("\nbtime "))
	line, _, _ := bytes.Cut(rest, []byte{'\n'})

	boot, err := strconv.ParseInt(string(line), 10, 64)
	if err != nil {
		return time.Time{}
	}

	return time.Unix(boot, 0).Add(time.Duration(ticks) * time.Second / 100) // USER_HZ, 100 on every Linux port
}

// claudeSchedules are the jobs of conversation c: its CronCreate calls that
// were answered with an id and not deleted, and its self-paced loop. Claude
// takes a transcript's jobs up again as it resumes it, but not its loop: one
// armed before since is gone.
// Nil until claudeSubagents has found its transcript.
func claudeSchedules(c proto.Conversation, since, now time.Time) []proto.Schedule {
	subs.Lock()
	sc := subs.m[c.ID]
	subs.Unlock()

	if sc == nil {
		return nil
	}

	return firing(scanSchedules(filepath.Dir(sc.dir)+".jsonl", claudeSchedule), since, now)
}

func claudeSchedule(s *schedScan, line []byte) {
	asked := bytes.Contains(line, []byte(`"name":"Cron`)) || bytes.Contains(line, []byte(`"name":"ScheduleWakeup"`))
	answered := slices.ContainsFunc(s.jobs, func(j schedJob) bool { return j.pending && bytes.Contains(line, []byte(j.call)) })

	if !asked && !answered { // the rest hold whole tool output
		return
	}

	var v struct {
		At      time.Time `json:"timestamp"`
		Message struct {
			Content []struct {
				Type   string `json:"type"`
				ID     string `json:"id"`
				Name   string `json:"name"`
				Answer string `json:"tool_use_id"`
				Input  struct {
					ID     string  `json:"id"`
					Cron   string  `json:"cron"`
					Prompt string  `json:"prompt"`
					Delay  float64 `json:"delaySeconds"`
					Stop   bool    `json:"stop"`
				} `json:"input"`
			} `json:"content"`
		} `json:"message"`
		Result json.RawMessage `json:"toolUseResult"`
	}

	if json.Unmarshal(line, &v) != nil { // a message of plain text that names a tool
		return
	}

	for _, c := range v.Message.Content {
		switch {
		case c.Type == "tool_result":
			s.claudeAnswer(c.Answer, v.Result)
		case c.Type != "tool_use":
		case c.Name == "CronCreate":
			s.jobs = append(s.jobs, schedJob{
				call: c.ID, pending: true, at: v.At, until: v.At.Add(scheduleExpiry),
				s: proto.Schedule{Every: c.Input.Cron, Prompt: c.Input.Prompt},
			})

		case c.Name == "CronDelete":
			s.cancel(c.Input.ID)
		case c.Name == "ScheduleWakeup" && c.Input.Stop:
			s.wake = schedJob{}
		case c.Name == "ScheduleWakeup":
			wake := v.At.Add(time.Duration(min(max(c.Input.Delay, 60), 3600) * float64(time.Second))) // claude's own bounds
			s.wake = schedJob{at: v.At, until: wake.Add(wakeSlack), s: proto.Schedule{Every: selfPaced, Prompt: c.Input.Prompt}}
		}
	}
}

// claudeAnswer takes what claude answered CronCreate call: the job's id, or
// no job, for a call refused or one that fires once.
func (s *schedScan) claudeAnswer(call string, result json.RawMessage) {
	i := slices.IndexFunc(s.jobs, func(j schedJob) bool { return j.pending && j.call == call })
	if i < 0 {
		return
	}

	var r struct {
		ID        string `json:"id"`
		Recurring bool   `json:"recurring"`
	}

	_ = json.Unmarshal(result, &r) // an error's is a string: no id

	if r.ID == "" || !r.Recurring {
		s.jobs = slices.Delete(s.jobs, i, i+1)

		return
	}

	s.jobs[i].id, s.jobs[i].durable, s.jobs[i].pending = r.ID, true, false
}

// copilotSchedules are the jobs of the session a copilot process holds in
// files, by its inuse.<pid>.hold: the /every and /loop prompts and the
// self-paced loops its events.jsonl registered and did not cancel. Copilot
// keeps them across a resume; a self-paced loop counts until a while past
// the run it last armed.
func copilotSchedules(files []string, now time.Time) []proto.Schedule {
	for _, p := range files {
		if base := filepath.Base(p); !strings.HasPrefix(base, "inuse.") || filepath.Ext(base) != ".hold" {
			continue
		}

		return firing(scanSchedules(filepath.Join(filepath.Dir(p), "events.jsonl"), copilotSchedule), time.Time{}, now)
	}

	return nil
}

func copilotSchedule(s *schedScan, line []byte) {
	if !bytes.Contains(line, []byte(`"type":"session.schedule_`)) { // the rest hold whole tool output
		return
	}

	var v struct {
		Type string    `json:"type"`
		At   time.Time `json:"timestamp"`
		Data struct {
			ID        int    `json:"id"`
			Cron      string `json:"cron"`
			Interval  int64  `json:"intervalMs"`
			Prompt    string `json:"prompt"`
			Shown     string `json:"displayPrompt"`
			Recurring bool   `json:"recurring"`
			SelfPaced bool   `json:"selfPaced"`
			Next      int64  `json:"nextRunAt"`
		} `json:"data"`
	}

	if json.Unmarshal(line, &v) != nil {
		return
	}

	id := strconv.Itoa(v.Data.ID)

	switch {
	case v.Type == "session.schedule_cancelled":
		s.cancel(id)
	case v.Type == "session.schedule_rearmed": // a self-paced loop ends by not arming its next run
		if i := s.find(id); i >= 0 {
			s.jobs[i].until = time.UnixMilli(v.Data.Next).Add(wakeSlack)
		}

	case v.Type == "session.schedule_created" && (v.Data.Recurring || v.Data.SelfPaced || v.Data.Cron != ""):
		s.cancel(id) // registered again by a resume

		every := cmp.Or(v.Data.Cron, interval(v.Data.Interval), selfPaced)
		prompt := cmp.Or(v.Data.Shown, v.Data.Prompt)

		job := schedJob{id: id, s: proto.Schedule{Every: every, Prompt: prompt}}
		if every == selfPaced { // ponytail: a first run armed further off than wakeSlack hides the loop until it comes
			job.until = v.At.Add(wakeSlack)
		}

		s.jobs = append(s.jobs, job)
	}
}

// interval writes ms milliseconds as an agent's own "5m" or "1h30m", "" for
// none.
func interval(ms int64) string {
	if ms <= 0 {
		return ""
	}

	d := (time.Duration(ms) * time.Millisecond).String() // "5m0s", "1h0m0s"
	if strings.HasSuffix(d, "m0s") {
		d = strings.TrimSuffix(d, "0s")
	}

	if strings.HasSuffix(d, "h0m") {
		d = strings.TrimSuffix(d, "0m")
	}

	return d
}

// grokSchedules are the jobs of the session a grok process holds open in
// files that is no subagent's: the tasks its updates.jsonl says were created
// and not deleted since. Grok says a task deleted as it is cancelled, as it
// runs out its seven days and as its process ends, and created again as a
// new process takes up a durable one: a task made before since died with a
// process that could not say so.
func grokSchedules(files []string, since, now time.Time) []proto.Schedule {
	for _, p := range files {
		if filepath.Base(p) != "events.jsonl" {
			continue
		}

		if _, _, kind := grokSummary(filepath.Dir(p)); kind != "subagent" {
			return firing(scanSchedules(filepath.Join(filepath.Dir(p), "updates.jsonl"), grokSchedule), since, now)
		}
	}

	return nil
}

func grokSchedule(s *schedScan, line []byte) {
	if !bytes.Contains(line, []byte(`"scheduled_task_`)) { // the rest hold whole tool output
		return
	}

	var v struct {
		At     int64 `json:"timestamp"`
		Params struct {
			Update struct {
				Kind   string `json:"sessionUpdate"`
				Task   string `json:"task_id"`
				Prompt string `json:"prompt"`
				Every  string `json:"human_schedule"`
			} `json:"update"`
		} `json:"params"`
	}

	if json.Unmarshal(line, &v) != nil {
		return
	}

	u, at := v.Params.Update, time.Unix(v.At, 0)

	switch u.Kind {
	case "scheduled_task_deleted":
		s.cancel(u.Task)
	case "scheduled_task_created": // again as a call changes it, and as a new process takes it up
		s.cancel(u.Task)

		every := strings.TrimPrefix(u.Every, "every ")
		s.jobs = append(s.jobs, schedJob{id: u.Task, at: at, until: at.Add(scheduleExpiry), s: proto.Schedule{Every: every, Prompt: u.Prompt}})
	}
}

// agySchedules are the jobs of the conversation an agy process has open,
// the one whose presence/<id>.lock it holds in files: the schedule calls
// with a cron expression its transcript answered with a task id, that no
// manage_task killed and no message said finished since. They live in agy's
// process.
func agySchedules(files []string, since, now time.Time) []proto.Schedule {
	for _, p := range files {
		if filepath.Base(filepath.Dir(p)) != "presence" || filepath.Ext(p) != ".lock" {
			continue
		}

		home, id := filepath.Dir(filepath.Dir(p)), strings.TrimSuffix(filepath.Base(p), ".lock")
		log := filepath.Join(home, "brain", id, ".system_generated", "logs", "transcript.jsonl")

		return firing(scanSchedules(log, agySchedule), since, now)
	}

	return nil
}

// agyFinished is how agy's message that a background task ended reads, and
// agyTask the id it names.
const agyFinished = "finished with result"

var agyTask = regexp.MustCompile(`Task id "([^"]+)" ` + agyFinished)

func agySchedule(s *schedScan, line []byte) {
	if len(s.calls) == 0 && !bytes.Contains(line, []byte(`"tool_calls"`)) && !bytes.Contains(line, []byte(agyFinished)) {
		return
	}

	var v struct {
		Step    int       `json:"step_index"`
		Type    string    `json:"type"`
		At      time.Time `json:"created_at"`
		Content string    `json:"content"`
		Calls   []struct {
			Name string                     `json:"name"`
			Args map[string]json.RawMessage `json:"args"`
		} `json:"tool_calls"`
	}

	if json.Unmarshal(line, &v) != nil {
		return
	}

	// A task that ended by itself, its MaxIterations run out, says so in a
	// message of its own.
	if m := agyTask.FindStringSubmatch(v.Content); v.Type == "SYSTEM_MESSAGE" && m != nil {
		s.cancel(m[1])

		return
	}

	// Each call of a reply is answered by a step of its own, in order: a
	// schedule's names the task a kill goes by, or why there is none.
	if len(v.Calls) == 0 {
		if v.Type != "GENERIC" || len(s.calls) == 0 {
			return
		}

		call := s.calls[0]
		s.calls = s.calls[1:]

		i := slices.IndexFunc(s.jobs, func(j schedJob) bool { return call != "" && j.call == call })

		switch {
		case i < 0:
		case strings.Contains(v.Content, "task id"):
			s.jobs[i].text, s.jobs[i].pending = []byte(v.Content), false
		default:
			s.jobs = slices.Delete(s.jobs, i, i+1)
		}

		return
	}

	s.calls = s.calls[:0]

	for n, c := range v.Calls {
		call := ""

		switch cron := agyArg(c.Args["CronExpression"]); {
		case c.Name == "schedule" && cron != "":
			call = strconv.Itoa(v.Step) + "." + strconv.Itoa(n)
			s.jobs = append(s.jobs, schedJob{call: call, pending: true, at: v.At, s: proto.Schedule{Every: cron, Prompt: agyArg(c.Args["Prompt"])}})

		case c.Name == "manage_task" && agyArg(c.Args["Action"]) == "kill":
			for name, arg := range c.Args {
				if name != "Action" && name != "toolSummary" {
					s.cancel(agyArg(arg))
				}
			}
		}

		s.calls = append(s.calls, call)
	}
}

// agyArg is a tool call's argument as the model gave it: agy writes each as
// a string that holds its JSON.
func agyArg(raw json.RawMessage) string {
	var s, inner string
	if json.Unmarshal(raw, &s) != nil {
		return ""
	}

	if json.Unmarshal([]byte(s), &inner) == nil {
		return inner
	}

	return s
}
