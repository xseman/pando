package daemon

import (
	"path/filepath"
	"testing"

	"github.com/xseman/pando/internal/proto"
)

func TestDrafts(t *testing.T) {
	boot := start(t)
	boot()

	ws := t.TempDir()
	file := filepath.Join(ws, "a.go")

	list := func() []proto.Draft {
		t.Helper()

		var out []proto.Draft
		if err := proto.Call("draft.list", map[string]string{"ws": ws}, &out); err != nil {
			t.Fatal(err)
		}

		return out
	}
	if got := list(); len(got) != 0 {
		t.Fatalf("a workspace with no drafts = %+v", got)
	}

	// A file's unsaved text and an untitled buffer are separate drafts.
	set := func(d proto.Draft) {
		t.Helper()

		if err := proto.Call("draft.set", d, nil); err != nil {
			t.Fatal(err)
		}
	}
	set(proto.Draft{WS: ws, Path: file, Text: "package a // wip\n"})
	set(proto.Draft{WS: ws, Name: "Untitled-1", Text: "notes\n"})

	byKey := func(ds []proto.Draft) map[string]string {
		out := map[string]string{}
		for _, d := range ds {
			out[d.Key()] = d.Text
		}

		return out
	}
	if got := byKey(list()); len(got) != 2 || got[file] != "package a // wip\n" || got["untitled:Untitled-1"] != "notes\n" {
		t.Fatalf("drafts = %v", got)
	}

	// Rewriting one leaves the other alone.
	set(proto.Draft{WS: ws, Name: "Untitled-1", Text: "notes, more\n"})

	if got := byKey(list()); len(got) != 2 || got["untitled:Untitled-1"] != "notes, more\n" {
		t.Fatalf("rewritten = %v", got)
	}

	// They survive a daemon that starts again: that is the whole point.
	boot()

	if got := byKey(list()); len(got) != 2 || got[file] != "package a // wip\n" {
		t.Fatalf("after restart = %v", got)
	}

	// An empty text forgets the draft, as it does for commit drafts.
	set(proto.Draft{WS: ws, Path: file})

	if got := byKey(list()); len(got) != 1 || got["untitled:Untitled-1"] == "" {
		t.Fatalf("after forgetting one = %v", got)
	}

	set(proto.Draft{WS: ws, Name: "Untitled-1"})

	if got := list(); len(got) != 0 {
		t.Fatalf("after forgetting both = %+v", got)
	}

	// Another workspace has its own drafts.
	other := t.TempDir()
	set(proto.Draft{WS: other, Name: "Untitled-1", Text: "elsewhere\n"})

	if got := list(); len(got) != 0 {
		t.Fatalf("drafts leaked between workspaces: %+v", got)
	}

	// A draft needs something to be keyed by.
	if err := proto.Call("draft.set", proto.Draft{WS: ws, Text: "x"}, nil); err == nil {
		t.Fatal("a draft with no path and no name is refused")
	}

	if err := proto.Call("draft.set", proto.Draft{Name: "Untitled-1", Text: "x"}, nil); err == nil {
		t.Fatal("a draft with no workspace is refused")
	}
}
