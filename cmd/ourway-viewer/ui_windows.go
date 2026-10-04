//go:build windows

package main

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"sync"
	"sync/atomic"
	"time"

	"gioui.org/app"
	"gioui.org/f32"
	"gioui.org/font/gofont"
	"gioui.org/io/event"
	"gioui.org/io/key"
	"gioui.org/io/pointer"
	"gioui.org/io/system"
	"gioui.org/layout"
	"gioui.org/op"
	"gioui.org/op/clip"
	"gioui.org/op/paint"
	"gioui.org/text"
	"gioui.org/unit"
	"gioui.org/widget"
	"gioui.org/widget/material"
)

const (
	monitorAll   = 255
	moveInterval = 16 * time.Millisecond // cap mouse-move traffic at ~60/s
)

// qualityLevels are the toolbar's JPEG quality presets.
var qualityLevels = []struct {
	name string
	q    int
}{{"Low", 40}, {"Medium", 65}, {"High", 85}}

// viewer is the native remote-session window.
type viewer struct {
	win    *app.Window
	th     *material.Theme
	launch Launch
	screen *Screen
	clip   *ClipSync
	syncOn atomic.Bool

	mu       sync.Mutex
	client   *Client
	monitors []Monitor
	status   string
	ended    bool
	selected int

	// toolbar widgets
	cadBtn      widget.Clickable
	monBtns     []widget.Clickable
	allBtn      widget.Clickable
	qualBtn     widget.Clickable
	qualIdx     int
	sendClipBtn widget.Clickable
	syncClip    widget.Bool
	endBtn      widget.Clickable

	// rendering / input state (UI goroutine only)
	imgOp    paint.ImageOp
	imgVer   uint64
	haveImg  bool
	inputTag struct{ _ byte }
	held     map[string]bool
	superOn  bool
	btnsDown pointer.Buttons
	lastMove time.Time
}

func newViewer(l Launch) *viewer {
	th := material.NewTheme()
	th.Shaper = text.NewShaper(text.WithCollection(gofont.Collection()))
	v := &viewer{
		win:      new(app.Window),
		th:       th,
		launch:   l,
		screen:   &Screen{},
		status:   "Connecting…",
		selected: 0,
		qualIdx:  len(qualityLevels) - 1,
		held:     map[string]bool{},
	}
	v.syncClip.Value = true
	v.syncOn.Store(true)
	title := "OurWay Remote"
	if l.Device != "" {
		title += " – " + l.Device
	}
	v.win.Option(app.Title(title), app.Size(unit.Dp(1280), unit.Dp(800)), app.MinSize(unit.Dp(640), unit.Dp(400)))
	return v
}

// connect dials in the background and wires the client's callbacks.
func (v *viewer) connect(ctx context.Context) {
	go func() {
		cl, err := Dial(ctx, v.launch, v.screen, Callbacks{
			Frame: func() {
				v.mu.Lock()
				v.status = ""
				v.mu.Unlock()
				v.win.Invalidate()
			},
			Monitors: func(m []Monitor) {
				v.mu.Lock()
				v.monitors = m
				v.mu.Unlock()
				v.win.Invalidate()
			},
			Clipboard: func(t string) {
				if v.syncOn.Load() {
					v.clip.Remote(t)
				}
			},
			Ended: func(reason string) {
				v.mu.Lock()
				v.ended, v.status = true, "Disconnected: "+reason
				v.mu.Unlock()
				v.win.Invalidate()
			},
		})
		if err != nil {
			v.mu.Lock()
			v.ended, v.status = true, err.Error()
			v.mu.Unlock()
			v.win.Invalidate()
			return
		}
		v.mu.Lock()
		v.client = cl
		v.status = "Waiting for the first frame…"
		v.mu.Unlock()
		v.clip = &ClipSync{Clip: winClipboard{}, Send: cl.SendClipboard}
		v.clip.Prime()
		go cl.Run()
		go func() { // local clipboard -> remote
			t := time.NewTicker(400 * time.Millisecond)
			defer t.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-t.C:
					if v.syncOn.Load() {
						v.clip.Poll()
					}
				}
			}
		}()
		v.win.Invalidate()
	}()
}

func (v *viewer) cl() *Client {
	v.mu.Lock()
	defer v.mu.Unlock()
	return v.client
}

// run is the window's event loop.
func (v *viewer) run() error {
	var ops op.Ops
	for {
		switch e := v.win.Event().(type) {
		case app.DestroyEvent:
			if c := v.cl(); c != nil {
				v.releaseAll(c)
				c.Close()
			}
			return e.Err
		case app.FrameEvent:
			gtx := app.NewContext(&ops, e)
			v.layout(gtx)
			e.Frame(gtx.Ops)
		}
	}
}

func (v *viewer) layout(gtx layout.Context) layout.Dimensions {
	v.syncOn.Store(v.syncClip.Value)
	v.handleButtons(gtx)
	return layout.Flex{Axis: layout.Vertical}.Layout(gtx,
		layout.Rigid(v.toolbar),
		layout.Flexed(1, v.screenArea),
	)
}

func (v *viewer) handleButtons(gtx layout.Context) {
	c := v.cl()
	if c == nil {
		return
	}
	if v.cadBtn.Clicked(gtx) {
		c.CtrlAltDel()
	}
	for i := range v.monBtns {
		if v.monBtns[i].Clicked(gtx) {
			v.selectMonitor(c, i)
		}
	}
	if v.allBtn.Clicked(gtx) {
		v.selectMonitor(c, monitorAll)
	}
	if v.qualBtn.Clicked(gtx) {
		v.qualIdx = (v.qualIdx + 1) % len(qualityLevels)
		c.SetQuality(qualityLevels[v.qualIdx].q)
	}
	if v.sendClipBtn.Clicked(gtx) {
		if t, err := (winClipboard{}).Get(); err == nil && t != "" && len(t) <= maxClipboardBytes {
			c.SendClipboard(t)
		}
	}
	if v.endBtn.Clicked(gtx) {
		v.win.Perform(system.ActionClose) // DestroyEvent then closes the client
	}
}

func (v *viewer) selectMonitor(c *Client, id int) {
	v.mu.Lock()
	v.selected = id
	v.mu.Unlock()
	c.SelectMonitor(id)
}

func (v *viewer) toolbar(gtx layout.Context) layout.Dimensions {
	v.mu.Lock()
	mons := v.monitors
	sel := v.selected
	status := v.status
	v.mu.Unlock()
	if len(v.monBtns) != len(mons) {
		v.monBtns = make([]widget.Clickable, len(mons))
	}

	gap := layout.Inset{Right: unit.Dp(6)}
	btn := func(c *widget.Clickable, label string, active bool) layout.FlexChild {
		return layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return gap.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
				b := material.Button(v.th, c, label)
				if active {
					b.Background = color.NRGBA{R: 0x1b, G: 0x7f, B: 0x3b, A: 0xff}
				}
				return b.Layout(gtx)
			})
		})
	}

	children := []layout.FlexChild{btn(&v.cadBtn, "Ctrl+Alt+Del", false)}
	if len(mons) > 1 {
		for i, m := range mons {
			label := fmt.Sprintf("Display %d", i+1)
			if m.Primary {
				label += " ★"
			}
			children = append(children, btn(&v.monBtns[i], label, sel == i))
		}
		children = append(children, btn(&v.allBtn, "All", sel == monitorAll))
	}
	children = append(children,
		btn(&v.qualBtn, "Quality: "+qualityLevels[v.qualIdx].name, false),
		btn(&v.sendClipBtn, "Send clipboard", false),
		layout.Rigid(func(gtx layout.Context) layout.Dimensions {
			return gap.Layout(gtx, material.CheckBox(v.th, &v.syncClip, "Sync clipboard").Layout)
		}),
		layout.Flexed(1, func(gtx layout.Context) layout.Dimensions {
			return layout.E.Layout(gtx, material.Body2(v.th, status).Layout)
		}),
		btn(&v.endBtn, "End", false),
	)
	return layout.UniformInset(unit.Dp(6)).Layout(gtx, func(gtx layout.Context) layout.Dimensions {
		return layout.Flex{Alignment: layout.Middle}.Layout(gtx, children...)
	})
}

// screenArea draws the remote screen scaled to fit and turns pointer and
// keyboard input over it into remote input.
func (v *viewer) screenArea(gtx layout.Context) layout.Dimensions {
	size := gtx.Constraints.Max
	paint.FillShape(gtx.Ops, color.NRGBA{A: 0xff}, clip.Rect{Max: size}.Op())

	if img, ver := v.screen.Snapshot(v.imgVer); img != nil {
		v.imgOp = paint.NewImageOp(img)
		v.imgOp.Filter = paint.FilterLinear
		v.imgVer, v.haveImg = ver, true
	}
	w, h := v.screen.Size()
	if !v.haveImg || w == 0 || h == 0 {
		return layout.Center.Layout(gtx, func(gtx layout.Context) layout.Dimensions {
			v.mu.Lock()
			s := v.status
			v.mu.Unlock()
			l := material.Body1(v.th, s)
			l.Color = color.NRGBA{R: 0xdd, G: 0xdd, B: 0xdd, A: 0xff}
			return l.Layout(gtx)
		})
	}

	scale := min(float32(size.X)/float32(w), float32(size.Y)/float32(h))
	dw, dh := int(float32(w)*scale), int(float32(h)*scale)
	origin := image.Pt((size.X-dw)/2, (size.Y-dh)/2)

	// Image.
	off := op.Offset(origin).Push(gtx.Ops)
	aff := op.Affine(f32.Affine2D{}.Scale(f32.Point{}, f32.Pt(scale, scale))).Push(gtx.Ops)
	cl := clip.Rect{Max: image.Pt(w, h)}.Push(gtx.Ops)
	v.imgOp.Add(gtx.Ops)
	paint.PaintOp{}.Add(gtx.Ops)
	cl.Pop()
	aff.Pop()
	off.Pop()

	// Input area, in displayed-pixel coordinates.
	off = op.Offset(origin).Push(gtx.Ops)
	area := clip.Rect{Max: image.Pt(dw, dh)}.Push(gtx.Ops)
	v.handleInput(gtx, dw, dh)
	event.Op(gtx.Ops, &v.inputTag)
	gtx.Execute(key.FocusCmd{Tag: &v.inputTag})
	area.Pop()
	off.Pop()
	return layout.Dimensions{Size: size}
}

const allMods = key.ModCtrl | key.ModAlt | key.ModShift | key.ModSuper

func (v *viewer) handleInput(gtx layout.Context, dw, dh int) {
	c := v.cl()
	tag := &v.inputTag
	for {
		ev, ok := gtx.Event(
			pointer.Filter{
				Target:  tag,
				Kinds:   pointer.Move | pointer.Press | pointer.Release | pointer.Drag | pointer.Scroll,
				ScrollY: pointer.ScrollRange{Min: -4000, Max: 4000},
			},
			key.Filter{Focus: tag, Optional: allMods},
			key.FocusFilter{Target: tag},
		)
		if !ok {
			return
		}
		if c == nil {
			continue
		}
		switch e := ev.(type) {
		case pointer.Event:
			v.pointerEvent(c, e, dw, dh)
		case key.Event:
			v.keyEvent(c, e)
		case key.FocusEvent:
			if !e.Focus {
				v.releaseAll(c) // never leave a key stuck down remotely
			}
		}
	}
}

func pct(p f32.Point, dw, dh int) (float64, float64) {
	return float64(p.X) / float64(dw) * 100, float64(p.Y) / float64(dh) * 100
}

func buttonName(b pointer.Buttons) string {
	switch {
	case b.Contain(pointer.ButtonSecondary):
		return "right"
	case b.Contain(pointer.ButtonTertiary):
		return "middle"
	}
	return "left"
}

func (v *viewer) pointerEvent(c *Client, e pointer.Event, dw, dh int) {
	x, y := pct(e.Position, dw, dh)
	switch e.Kind {
	case pointer.Move, pointer.Drag:
		if time.Since(v.lastMove) >= moveInterval {
			v.lastMove = time.Now()
			c.Mouse("move", x, y, "")
		}
	case pointer.Press:
		v.btnsDown = e.Buttons
		c.Mouse("down", x, y, buttonName(e.Buttons))
	case pointer.Release:
		released := v.btnsDown &^ e.Buttons
		v.btnsDown = e.Buttons
		if released == 0 {
			released = pointer.ButtonPrimary
		}
		c.Mouse("up", x, y, buttonName(released))
	case pointer.Scroll:
		if e.Scroll.Y != 0 {
			// Gio reports 120 per notch (down positive); the agent
			// expects ~100 per notch.
			c.Wheel(float64(e.Scroll.Y) * 100 / 120)
		}
	}
}

func (v *viewer) keyEvent(c *Client, e key.Event) {
	// Gio does not report the Windows key as a key event, only as a
	// modifier, so mirror it from the modifier set.
	if sup := e.Modifiers.Contain(key.ModSuper); sup != v.superOn {
		v.superOn = sup
		c.Key("meta", sup)
	}
	name, ok := browserKey(string(e.Name))
	if !ok {
		return
	}
	switch e.State {
	case key.Press:
		v.held[name] = true
		c.Key(name, true)
	case key.Release:
		delete(v.held, name)
		c.Key(name, false)
	}
}

// releaseAll sends key-up for everything still held.
func (v *viewer) releaseAll(c *Client) {
	for k := range v.held {
		c.Key(k, false)
		delete(v.held, k)
	}
	if v.superOn {
		v.superOn = false
		c.Key("meta", false)
	}
	if v.btnsDown != 0 {
		c.Mouse("up", 0, 0, buttonName(v.btnsDown))
		v.btnsDown = 0
	}
}
