package ui

import (
	"math"
	"math/rand"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/harmonica"
	"github.com/charmbracelet/x/ansi"

	"cashstratumapi/cmd/cashtop/internal/termcap"
	"cashstratumapi/cmd/cashtop/internal/ui/glyphs"
)

const (
	celebrationTicks = 24 // ~6 s at one tick per 250 ms
	maxParticles     = 48
	burstEvery       = 8 // a new burst every this many ticks
	burstSize        = 24
)

func init() {
	registerOverlay(func(*Context) Overlay { return &celebration{} })
}

type particle struct {
	x, y, vx, vy float64
	life         int
	glyph        int
}

// celebration is the block-found takeover.
type celebration struct {
	active    bool
	tick      int
	block     BlockRow
	total     int
	rig       glyphs.Class
	counter   float64
	counterV  float64
	spring    harmonica.Spring
	rng       *rand.Rand
	particles []particle
}

func (c *celebration) Active() bool { return c.active }

func (c *celebration) Update(msg tea.Msg, ctx *Context) tea.Cmd {
	switch m := msg.(type) {
	case BlockFoundMsg:
		c.start(m, ctx)
	case BlockUpdateMsg:
		if c.active && c.block.Height == m.Height {
			if m.Luck > 0 {
				c.block.Luck = m.Luck
			}
			if m.Hash != "" {
				c.block.Hash = m.Hash
			}
		}
	case SnapshotMsg:
		if c.active {
			for _, b := range m.Snapshot.Blocks {
				if b.Height == c.block.Height {
					if b.Luck > 0 {
						c.block.Luck = b.Luck
					}
					if b.Reward > 0 {
						c.block.Reward = b.Reward
					}
					if b.Hash != "" {
						c.block.Hash = b.Hash
					}
					break
				}
			}
		}
	case TickMsg:
		if c.active {
			c.advance(ctx)
		}
	case tea.KeyPressMsg:
		if c.active && m.String() != "ctrl+c" {
			c.active = false
			c.particles = c.particles[:0]
		}
	}
	return nil
}

func (c *celebration) start(m BlockFoundMsg, ctx *Context) {
	c.active = true
	c.tick = 0
	c.block = m.Block
	c.rig = glyphs.Unknown
	for _, w := range ctx.Snap.Workers {
		if w.Name == m.Block.Finder {
			c.rig = glyphs.Infer(w.UserAgent, w.Name)
			break
		}
	}
	n := len(ctx.Snap.Blocks)
	known := false
	for _, b := range ctx.Snap.Blocks {
		if b.Height == m.Block.Height {
			known = true
			break
		}
	}
	if !known {
		n++
	}
	c.total = n
	c.spring = harmonica.NewSpring(harmonica.FPS(4), 6.0, 0.7)
	c.counter, c.counterV = float64(n-1), 0
	c.rng = rand.New(rand.NewSource(int64(m.Block.Height)))
	c.particles = c.particles[:0]
	if c.animated(ctx) {
		c.burst(ctx)
	}
}

func (c *celebration) animated(ctx *Context) bool {
	return ctx.Motion && ctx.Caps.Glyphs != termcap.ASCII
}

func (c *celebration) advance(ctx *Context) {
	c.tick++
	if c.tick >= celebrationTicks {
		c.active = false
		c.particles = c.particles[:0]
		return
	}
	if !c.animated(ctx) {
		return
	}
	c.counter, c.counterV = c.spring.Update(c.counter, c.counterV, float64(c.total))
	live := c.particles[:0]
	for _, p := range c.particles {
		p.x += p.vx
		p.y += p.vy
		p.vy += 0.25
		p.life--
		if p.life > 0 {
			live = append(live, p)
		}
	}
	c.particles = live
	if c.tick%burstEvery == 0 && c.tick < celebrationTicks-6 {
		c.burst(ctx)
	}
}

// burst launches a ring of particles, never exceeding maxParticles in total.
func (c *celebration) burst(ctx *Context) {
	cx := 0.2 + 0.6*c.rng.Float64()
	cy := 0.2 + 0.3*c.rng.Float64()
	for i := 0; i < burstSize && len(c.particles) < maxParticles; i++ {
		a := 2 * math.Pi * float64(i) / burstSize
		sp := 0.8 + c.rng.Float64()*0.8
		c.particles = append(c.particles, particle{
			x: cx * 1000, y: cy * 100,
			vx: math.Cos(a) * sp * 14, vy: math.Sin(a) * sp * 3.6,
			life: 5 + c.rng.Intn(3), glyph: c.rng.Intn(3),
		})
	}
}

// bigFont is a 3-row digit font, block variant then ASCII variant.
var bigFont = [2][10][3]string{
	{
		{"█▀█", "█ █", "▀▀▀"}, {" ▄█", "  █", "  ▀"}, {"▀▀█", "█▀▀", "▀▀▀"}, {"▀▀█", " ▀█", "▀▀▀"}, {"█ █", "▀▀█", "  ▀"},
		{"█▀▀", "▀▀█", "▀▀▀"}, {"█▀▀", "█▀█", "▀▀▀"}, {"▀▀█", "  █", "  ▀"}, {"█▀█", "█▀█", "▀▀▀"}, {"█▀█", "▀▀█", "▀▀▀"},
	},
	{
		{"###", "# #", "###"}, {" ##", "  #", "  #"}, {"###", " ##", "###"}, {"###", " ##", "###"}, {"# #", "###", "  #"},
		{"###", "## ", "###"}, {"#  ", "###", "###"}, {"###", "  #", "  #"}, {"###", "###", "###"}, {"###", "###", "  #"},
	},
}

func bigDigits(n int, ascii bool) []string {
	v := 0
	if ascii {
		v = 1
	}
	rows := make([]string, 3)
	for _, r := range strconv.Itoa(n) {
		d := int(r - '0')
		if d < 0 || d > 9 {
			continue
		}
		for i := range rows {
			rows[i] += bigFont[v][d][i] + " "
		}
	}
	for i := range rows {
		rows[i] = strings.TrimRight(rows[i], " ")
	}
	return rows
}

func (c *celebration) Render(ctx *Context, width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	ascii := ctx.Caps.Glyphs == termcap.ASCII
	th := ctx.Theme
	grid := make([][]rune, height)
	for i := range grid {
		grid[i] = []rune(strings.Repeat(" ", width))
	}
	// fireworks sit behind the text
	spark := []rune{'*', '+', '.'}
	if !ascii {
		spark = []rune{'✦', '✧', '·'}
	}
	for _, p := range c.particles {
		x, y := int(p.x)*width/1000, int(p.y)*height/100
		if x >= 0 && x < width && y >= 0 && y < height {
			grid[y][x] = spark[p.glyph%3]
		}
	}
	var lines []string
	for _, r := range grid {
		lines = append(lines, th.Gold.Render(string(r)))
	}

	put := func(row int, s string) {
		if row >= 0 && row < height {
			pad := (width - ansi.StringWidth(s)) / 2
			if pad < 0 {
				pad = 0
			}
			lines[row] = fit(strings.Repeat(" ", pad)+s, width)
		}
	}
	banner := " BLOCK FOUND "
	if !ascii {
		banner = "★ BLOCK FOUND ★"
	}
	top := (height - 14) / 2
	if top < 0 {
		top = 0
	}
	put(top, th.Gold.Render(strings.Repeat("=", 4)+" "+banner+" "+strings.Repeat("=", 4)))
	for i, r := range bigDigits(c.block.Height, ascii) {
		put(top+2+i, th.Gold.Render(r))
	}
	row := top + 6
	finder := shortWorker(c.block.Finder, ctx.sym.Ellipsis)
	if finder == "" {
		finder = dash
	}
	put(row, th.Text.Render("found by ")+th.Accent.Render(glyphs.Badge(c.rig, ascii)+" "+finder)+th.Dim.Render("  "+c.rig.Tag()))
	reward := "reward pending"
	if c.block.Reward > 0 {
		reward = "reward " + fmtBCH(c.block.Reward)
	}
	put(row+1, th.Text.Render(reward+"   luck "+fmtPct(c.block.Luck)))
	count := int(math.Round(c.counter))
	if !c.animated(ctx) {
		count = c.total
	}
	put(row+3, th.Dim.Render("blocks found ")+th.Gold.Render(strconv.Itoa(count)))
	put(row+5, th.Dim.Render("press any key to dismiss"))
	return strings.Join(lines, "\n")
}
