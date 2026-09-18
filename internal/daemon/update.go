package daemon

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/xseman/pando/internal/proto"
	"github.com/xseman/pando/internal/update"
)

// checkEvery is how often the daemon asks GitHub whether a newer pando was
// released. The first check runs at startup.
const checkEvery = 24 * time.Hour

// Update states, the values of proto.Update.State.
const (
	upChecking    = "checking"
	upAvailable   = "available"
	upCurrent     = "current"
	upDownloading = "downloading"
	upReady       = "ready"
	upError       = "error"
)

// WatchUpdates checks at startup and once a day after, while update_check is
// on, until the daemon closes. A failed check stays in the status and is
// retried at the next tick, so an offline machine never fills the log.
//
// `pando serve` starts it; a daemon a test drives directly never reaches for
// the network on its own.
func (d *Daemon) WatchUpdates() {
	for {
		d.mu.Lock()
		// A build with no version linked in is never behind a release, so it
		// has nothing to check for; that also keeps `go run` and the tests
		// off the network.
		on := d.state.Settings.Updates && update.Version != update.Dev
		d.mu.Unlock()

		if on {
			_ = d.checkUpdate()
		}

		select {
		case <-d.ctx.Done():
			return
		case <-time.After(checkEvery):
		}
	}
}

// setUpdate applies f to the update status and pushes the result to every
// subscriber. broadcast takes the lock itself, so it runs outside it.
func (d *Daemon) setUpdate(f func(u *proto.Update)) {
	d.mu.Lock()
	f(&d.upd)
	u := d.upd
	d.mu.Unlock()

	data, _ := json.Marshal(u) // Update is strings and ints
	d.broadcast(proto.Event{Kind: "update", Data: data})
}

// updateStatus is what the daemon knows now, without asking GitHub.
func (d *Daemon) updateStatus() proto.Update {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.upd
}

// checkUpdate asks GitHub for the latest release. A download in flight, and a
// binary already replaced, are left alone: the answer would only undo them.
func (d *Daemon) checkUpdate() error {
	if s := d.updateStatus().State; s == upDownloading || s == upReady {
		return nil
	}

	d.setUpdate(func(u *proto.Update) { u.State = upChecking })
	rel, err := update.Check(d.ctx)
	d.mu.Lock()
	if err == nil {
		d.rel = rel
	}
	d.mu.Unlock()
	d.setUpdate(func(u *proto.Update) {
		u.Error = ""

		switch {
		case err != nil:
			u.State, u.Error = upError, err.Error()
		case rel.Newer():
			u.State, u.Latest, u.Notes = upAvailable, rel.Version, rel.Notes
		default:
			u.State, u.Latest, u.Notes = upCurrent, rel.Version, ""
		}
	})

	return err
}

// installUpdate downloads the release the last check found and puts it where
// this executable is, reporting progress as `update` events. It returns as
// soon as the download starts: the caller follows it through the events.
func (d *Daemon) installUpdate() error {
	d.mu.Lock()
	rel, state := d.rel, d.upd.State
	d.mu.Unlock()

	switch {
	case state == upDownloading:
		return errors.New("an update is already downloading")
	case rel.URL == "":
		return errors.New("nothing checked yet: call update.check first")
	}

	d.setUpdate(func(u *proto.Update) {
		u.State, u.Done, u.Total, u.Error = upDownloading, 0, 0, ""
	})
	go func() {
		err := update.Install(d.ctx, rel, func(done, total int64) {
			d.setUpdate(func(u *proto.Update) { u.Done, u.Total = done, total })
		})
		d.setUpdate(func(u *proto.Update) {
			if err != nil {
				u.State, u.Error = upError, err.Error()
				return
			}

			u.State, u.Latest, u.Error = upReady, rel.Version, ""
		})
	}()

	return nil
}
