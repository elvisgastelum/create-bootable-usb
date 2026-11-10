// Package tui is the interactive terminal interface: choose an image,
// choose a USB device, confirm, and watch the write.
package tui

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/elvisgastelum/create-bootable-usb/internal/burn"
	"github.com/elvisgastelum/create-bootable-usb/internal/device"
	"github.com/elvisgastelum/create-bootable-usb/internal/images"
)

// rescanInterval is how often the device list refreshes on its own, so a
// stick plugged in while the list is open shows up without a keypress.
const rescanInterval = 3 * time.Second

// Deps are the operations the interface needs; main wires the real ones.
type Deps struct {
	Version     string
	Home        string // shown as ~ and used to expand typed paths
	ListDevices func() ([]device.Device, error)
	FindImages  func() []images.Candidate
	Inspect     func(path string) (images.Info, error)
	Burn        func(ctx context.Context, job burn.Job, report func(burn.Progress)) (burn.Result, error)
}

// Options are choices made on the command line.
type Options struct {
	Image string // image path given as an argument; skips the image screen
}

type screen int

const (
	screenImage screen = iota
	screenPath
	screenDevice
	screenConfirm
	screenWriting
	screenDone
	screenFailed
)

type (
	devicesMsg struct {
		gen     int
		devices []device.Device
		err     error
	}
	rescanMsg   struct{ gen int }
	progressMsg burn.Progress
	finishedMsg struct {
		result burn.Result
		err    error
	}
)

// blockedDevice is a detected device that cannot be selected.
type blockedDevice struct {
	device device.Device
	reason string
}

// Model is the Bubble Tea model for the whole flow.
type Model struct {
	deps   Deps
	screen screen
	width  int

	candidates  []images.Candidate
	imageCursor int
	imageErr    string
	pathInput   textinput.Model
	pathErr     string
	image       images.Info

	devices      []device.Device
	deviceCursor int
	scanning     bool
	scanGen      int
	scanErr      string
	spinner      spinner.Model

	selected     device.Device
	verify       bool
	confirmInput textinput.Model
	confirmErr   string

	bar         progress.Model
	events      chan tea.Msg
	cancel      context.CancelFunc
	stage       burn.Stage
	done, total int64
	stageStart  time.Time
	cancelArmed bool
	cancelling  bool

	result    burn.Result
	err       error
	cancelled bool
}

// New builds the model. With opts.Image set the flow starts at the device
// screen; the image must already have been validated by the caller.
func New(deps Deps, opts Options) Model {
	path := textinput.New()
	path.Placeholder = "/path/to/image.iso"
	path.Prompt = "› "
	path.CharLimit = 4096
	path.Width = 60

	confirm := textinput.New()
	confirm.Prompt = "› "
	confirm.CharLimit = 8
	confirm.Width = 10

	m := Model{
		deps:         deps,
		width:        80,
		candidates:   deps.FindImages(),
		pathInput:    path,
		confirmInput: confirm,
		spinner:      spinner.New(spinner.WithSpinner(spinner.Dot), spinner.WithStyle(accentStyle)),
		bar:          progress.New(progress.WithDefaultGradient(), progress.WithoutPercentage()),
		verify:       true,
	}
	if opts.Image != "" {
		if info, err := deps.Inspect(opts.Image); err == nil {
			m.image = info
			m.screen = screenDevice
		} else {
			m.imageErr = err.Error()
		}
	}
	return m
}

// Filter is a Bubble Tea event filter. While a write runs, SIGINT and
// SIGTERM (which Bubble Tea turns into InterruptMsg and QuitMsg) become a
// ctrl+c key, so they need the same two presses to stop a write instead of
// quitting halfway through it.
func Filter(model tea.Model, msg tea.Msg) tea.Msg {
	m, ok := model.(Model)
	if !ok || m.screen != screenWriting {
		return msg
	}
	switch msg.(type) {
	case tea.QuitMsg, tea.InterruptMsg:
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return msg
}

// Init starts the spinner and, when the image is preselected, the scan.
func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.spinner.Tick}
	if m.screen == screenDevice {
		cmds = append(cmds, m.startScan())
	}
	return tea.Batch(cmds...)
}

// Update handles one message.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		return m, nil
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case devicesMsg:
		return m.onDevices(msg)
	case rescanMsg:
		if m.screen == screenDevice && msg.gen == m.scanGen && !m.scanning {
			return m, m.startScan()
		}
		return m, nil
	case progressMsg:
		if m.screen != screenWriting {
			return m, nil
		}
		m.onProgress(burn.Progress(msg))
		return m, waitFor(m.events)
	case finishedMsg:
		return m.onFinished(msg)
	case tea.KeyMsg:
		return m.onKey(msg)
	}
	return m.updateInputs(msg)
}

func (m Model) updateInputs(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd
	switch m.screen {
	case screenPath:
		m.pathInput, cmd = m.pathInput.Update(msg)
	case screenConfirm:
		m.confirmInput, cmd = m.confirmInput.Update(msg)
	}
	return m, cmd
}

func (m Model) onKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := msg.String()
	if k == "ctrl+c" && m.screen != screenWriting {
		return m, tea.Quit
	}
	switch m.screen {
	case screenImage:
		return m.onImageKey(k)
	case screenPath:
		return m.onPathKey(msg)
	case screenDevice:
		return m.onDeviceKey(k)
	case screenConfirm:
		return m.onConfirmKey(msg)
	case screenWriting:
		return m.onWritingKey(k)
	case screenDone:
		switch k {
		case "n":
			return m, m.enterDevices()
		case "enter", "q", "esc":
			return m, tea.Quit
		}
	case screenFailed:
		switch k {
		case "enter", "esc", "r":
			return m, m.enterDevices()
		case "q":
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m Model) onImageKey(k string) (tea.Model, tea.Cmd) {
	last := len(m.candidates) // the "Type a path…" entry
	switch k {
	case "up", "k":
		m.imageCursor = max(0, m.imageCursor-1)
	case "down", "j":
		m.imageCursor = min(last, m.imageCursor+1)
	case "q", "esc":
		return m, tea.Quit
	case "enter":
		if m.imageCursor == last {
			m.screen = screenPath
			m.pathErr = ""
			return m, tea.Batch(m.pathInput.Focus(), textinput.Blink)
		}
		info, err := m.deps.Inspect(m.candidates[m.imageCursor].Path)
		if err != nil {
			m.imageErr = err.Error()
			return m, nil
		}
		m.image, m.imageErr = info, ""
		return m, m.enterDevices()
	}
	return m, nil
}

func (m Model) onPathKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.pathInput.Blur()
		m.screen = screenImage
		return m, nil
	case "enter":
		path := images.Expand(m.pathInput.Value(), m.deps.Home)
		if path == "" {
			m.pathErr = "Type the path of an .iso or .img file."
			return m, nil
		}
		info, err := m.deps.Inspect(path)
		if err != nil {
			m.pathErr = err.Error()
			return m, nil
		}
		m.pathInput.Blur()
		m.image, m.pathErr = info, ""
		return m, m.enterDevices()
	}
	var cmd tea.Cmd
	m.pathInput, cmd = m.pathInput.Update(msg)
	return m, cmd
}

func (m Model) onDeviceKey(k string) (tea.Model, tea.Cmd) {
	ok, _ := m.targets()
	switch k {
	case "up", "k":
		m.deviceCursor = max(0, m.deviceCursor-1)
	case "down", "j":
		m.deviceCursor = max(0, min(len(ok)-1, m.deviceCursor+1))
	case "r":
		m.scanGen++
		return m, m.startScan()
	case "esc":
		m.scanGen++ // stop the automatic refresh
		m.screen = screenImage
		return m, nil
	case "q":
		return m, tea.Quit
	case "enter":
		if len(ok) == 0 {
			return m, nil
		}
		m.scanGen++
		m.selected = ok[m.deviceCursor]
		m.screen = screenConfirm
		m.confirmErr = ""
		m.confirmInput.Reset()
		return m, tea.Batch(m.confirmInput.Focus(), textinput.Blink)
	}
	return m, nil
}

func (m Model) onConfirmKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.confirmInput.Blur()
		return m, m.enterDevices()
	case "tab":
		m.verify = !m.verify
		return m, nil
	case "enter":
		if !strings.EqualFold(strings.TrimSpace(m.confirmInput.Value()), "yes") {
			m.confirmErr = "Type yes to continue, or press esc to go back."
			return m, nil
		}
		m.confirmInput.Blur()
		return m, m.startWrite()
	}
	var cmd tea.Cmd
	m.confirmInput, cmd = m.confirmInput.Update(msg)
	return m, cmd
}

func (m Model) onWritingKey(k string) (tea.Model, tea.Cmd) {
	switch {
	case k == "ctrl+c" && m.cancelArmed && !m.cancelling:
		m.cancelling = true
		m.cancel()
	case k == "ctrl+c":
		m.cancelArmed = true
	case k == "esc":
		m.cancelArmed = false
	}
	return m, nil
}

// enterDevices shows the device screen and scans right away.
func (m *Model) enterDevices() tea.Cmd {
	m.screen = screenDevice
	m.deviceCursor = 0
	m.scanGen++
	return m.startScan()
}

func (m *Model) startScan() tea.Cmd {
	m.scanning = true
	gen, list := m.scanGen, m.deps.ListDevices
	return func() tea.Msg {
		devices, err := list()
		return devicesMsg{gen: gen, devices: devices, err: err}
	}
}

func (m Model) onDevices(msg devicesMsg) (tea.Model, tea.Cmd) {
	if msg.gen != m.scanGen {
		return m, nil // a newer scan is on its way
	}
	m.scanning = false
	if msg.err != nil {
		m.scanErr = msg.err.Error()
	} else {
		ok, _ := m.targets()
		var highlighted string
		if m.deviceCursor < len(ok) {
			highlighted = ok[m.deviceCursor].Path
		}
		m.devices, m.scanErr = msg.devices, ""
		ok, _ = m.targets()
		m.deviceCursor = 0
		for i, d := range ok {
			if d.Path == highlighted {
				m.deviceCursor = i
			}
		}
	}
	if m.screen != screenDevice {
		return m, nil
	}
	gen := m.scanGen
	return m, tea.Tick(rescanInterval, func(time.Time) tea.Msg { return rescanMsg{gen: gen} })
}

// targets splits the detected devices into selectable ones and ones that
// are shown with the reason they cannot be used.
func (m Model) targets() (ok []device.Device, blocked []blockedDevice) {
	for _, d := range m.devices {
		reason := d.Problem(m.image.Size)
		if reason == "" && burn.ImageOnDevice(m.image.Path, d) {
			reason = "contains the selected image"
		}
		if reason == "" {
			ok = append(ok, d)
		} else {
			blocked = append(blocked, blockedDevice{d, reason})
		}
	}
	return ok, blocked
}

func (m *Model) startWrite() tea.Cmd {
	ctx, cancel := context.WithCancel(context.Background())
	events := make(chan tea.Msg, 64)
	m.screen = screenWriting
	m.cancel, m.events = cancel, events
	m.stage, m.done, m.total = burn.StagePreparing, 0, 0
	m.stageStart = time.Now()
	m.cancelArmed, m.cancelling = false, false
	m.err, m.cancelled = nil, false

	job := burn.Job{Image: m.image.Path, Device: m.selected, Verify: m.verify}
	run := m.deps.Burn
	return tea.Batch(
		func() tea.Msg {
			res, err := run(ctx, job, func(p burn.Progress) {
				select { // progress is best effort; the UI catches up on the next report
				case events <- progressMsg(p):
				default:
				}
			})
			events <- finishedMsg{result: res, err: err}
			return nil
		},
		waitFor(events),
	)
}

func waitFor(events chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-events }
}

func (m *Model) onProgress(p burn.Progress) {
	if p.Stage != m.stage {
		m.stage = p.Stage
		m.stageStart = time.Now()
	}
	m.done, m.total = p.Done, p.Total
}

func (m Model) onFinished(msg finishedMsg) (tea.Model, tea.Cmd) {
	if m.cancel != nil {
		m.cancel()
	}
	if msg.err != nil {
		m.screen = screenFailed
		m.err = msg.err
		m.cancelled = errors.Is(msg.err, context.Canceled)
		return m, nil
	}
	m.screen = screenDone
	m.result = msg.result
	return m, nil
}
