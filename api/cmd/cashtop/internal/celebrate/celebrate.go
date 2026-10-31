// Package celebrate produces the terminal side effects of a block find: three
// bells and a desktop notification, all through tea.Raw.
package celebrate

import (
	"fmt"
	"time"

	tea "charm.land/bubbletea/v2"

	"cashstratumapi/cmd/cashtop/internal/events"
	"cashstratumapi/cmd/cashtop/internal/termcap"
)

const (
	rings    = 3
	interval = 300 * time.Millisecond
)

// Step is one raw write, issued after Delay from the previous step.
type Step struct {
	Delay time.Duration
	Data  string
}

// Text returns the notification title and body for a find.
func Text(b events.BlockFound) (title, body string) {
	title = fmt.Sprintf("Block %d found!", b.Height)
	body = b.Finder
	if b.HasReward {
		body += fmt.Sprintf(" - %.8g BCH", b.Reward)
	}
	return title, body
}

// Plan lists the writes for a find, or nil when notifications are off.
func Plan(c termcap.Caps, b events.BlockFound) []Step {
	title, body := Text(b)
	seqs := termcap.Notifications(c, title, body)
	if len(seqs) == 0 {
		return nil
	}
	var steps []Step
	for _, s := range seqs {
		if s != termcap.BEL() {
			steps = append(steps, Step{Data: s})
		}
	}
	for i := 0; i < rings; i++ {
		d := interval
		if i == 0 {
			d = 0
		}
		steps = append(steps, Step{Delay: d, Data: termcap.BEL()})
	}
	return steps
}

// Cmd returns the command that plays Plan, or nil when there is nothing to emit.
func Cmd(c termcap.Caps, b events.BlockFound) tea.Cmd {
	steps := Plan(c, b)
	if len(steps) == 0 {
		return nil
	}
	cmds := make([]tea.Cmd, 0, len(steps))
	for _, s := range steps {
		s := s
		if s.Delay == 0 {
			cmds = append(cmds, tea.Raw(s.Data))
			continue
		}
		cmds = append(cmds, tea.Tick(s.Delay, func(time.Time) tea.Msg { return tea.RawMsg{Msg: s.Data} }))
	}
	return tea.Sequence(cmds...)
}
