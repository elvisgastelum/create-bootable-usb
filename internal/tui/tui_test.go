package tui

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elvisgastelum/create-bootable-usb/internal/burn"
	"github.com/elvisgastelum/create-bootable-usb/internal/device"
	"github.com/elvisgastelum/create-bootable-usb/internal/images"
)

const isoSize = 1_706_178_560

var (
	stick = device.Device{ID: "disk6", Path: "/dev/disk6", RawPath: "/dev/rdisk6", Model: "ProductCode",
		Size: 16_043_212_800, Bus: "USB", Removable: true, Labels: []string{"USB"}, Mounts: []string{"/Volumes/USB"}}
	nvme = device.Device{ID: "disk4", Path: "/dev/disk4", Model: "KINGSTON SNV3S2000G",
		Size: 2_000_398_934_016, Bus: "PCI-Express", Labels: []string{"ExternalSSD"}}
	tiny = device.Device{ID: "disk7", Path: "/dev/disk7", Model: "Old Stick", Size: 1_000_000_000, Bus: "USB", Removable: true}
)

type fakeBurn struct {
	jobs  chan burn.Job
	block bool // wait for cancellation instead of finishing
}

func (f *fakeBurn) run(ctx context.Context, job burn.Job, report func(burn.Progress)) (burn.Result, error) {
	f.jobs <- job
	report(burn.Progress{Stage: burn.StageWriting, Done: isoSize / 2, Total: isoSize})
	if f.block {
		<-ctx.Done()
		return burn.Result{}, ctx.Err()
	}
	report(burn.Progress{Stage: burn.StageWriting, Done: isoSize, Total: isoSize})
	return burn.Result{SHA256: "abc123", Verified: job.Verify, Duration: 72 * time.Second}, nil
}

func newModel(t *testing.T, devices []device.Device, fb *fakeBurn) Model {
	t.Helper()
	deps := Deps{
		Version:     "test",
		Home:        "/Users/dev",
		ListDevices: func() ([]device.Device, error) { return devices, nil },
		FindImages: func() []images.Candidate {
			return []images.Candidate{{Path: "/Users/dev/Downloads/proxmox-ve_9.2-1.iso", Size: isoSize, Modified: time.Now()}}
		},
		Inspect: func(path string) (images.Info, error) {
			if strings.HasSuffix(path, "proxmox-ve_9.2-1.iso") {
				return images.Info{Path: path, Size: isoSize, Hybrid: true}, nil
			}
			return images.Info{}, errors.New(path + " does not exist")
		},
		Burn: fb.run,
	}
	return New(deps, Options{})
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case "down":
		return tea.KeyMsg{Type: tea.KeyDown}
	case "ctrl+c":
		return tea.KeyMsg{Type: tea.KeyCtrlC}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

func update(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

func typeText(t *testing.T, m Model, s string) Model {
	t.Helper()
	for _, r := range s {
		m, _ = update(t, m, key(string(r)))
	}
	return m
}

// toDeviceScreen picks the discovered ISO and delivers the device scan.
func toDeviceScreen(t *testing.T, m Model) Model {
	t.Helper()
	m, _ = update(t, m, key("enter"))
	if m.screen != screenDevice {
		t.Fatalf("screen = %v after choosing the image, want the device screen", m.screen)
	}
	devices, err := m.deps.ListDevices()
	m, _ = update(t, m, devicesMsg{gen: m.scanGen, devices: devices, err: err})
	return m
}

func toConfirmScreen(t *testing.T, m Model) Model {
	t.Helper()
	m = toDeviceScreen(t, m)
	m, _ = update(t, m, key("enter"))
	if m.screen != screenConfirm {
		t.Fatalf("screen = %v after choosing the device, want the confirm screen", m.screen)
	}
	return m
}

// collect runs cmd once and returns the commands it batches, so a test can
// start them concurrently like Bubble Tea does.
func collect(cmd tea.Cmd) []tea.Cmd {
	if cmd == nil {
		return nil
	}
	msg := cmd()
	if batch, ok := msg.(tea.BatchMsg); ok {
		var out []tea.Cmd
		for _, c := range batch {
			out = append(out, c)
		}
		return out
	}
	return []tea.Cmd{func() tea.Msg { return msg }}
}

func TestDeviceScreenOffersOnlyEligibleDevices(t *testing.T) {
	m := toDeviceScreen(t, newModel(t, []device.Device{nvme, stick, tiny}, &fakeBurn{}))

	ok, blocked := m.targets()
	if len(ok) != 1 || ok[0].Path != "/dev/disk6" {
		t.Fatalf("selectable devices = %+v, want only /dev/disk6", ok)
	}
	if len(blocked) != 2 {
		t.Fatalf("unavailable devices = %+v, want disk4 and disk7", blocked)
	}
	view := m.View()
	for _, want := range []string{"/dev/disk6", `"USB"`, "not a removable or USB device", "too small"} {
		if !strings.Contains(view, want) {
			t.Errorf("device screen is missing %q:\n%s", want, view)
		}
	}
}

func TestImageStoredOnTheStickIsNotOffered(t *testing.T) {
	m := newModel(t, []device.Device{stick}, &fakeBurn{})
	m.deps.FindImages = func() []images.Candidate {
		return []images.Candidate{{Path: "/Volumes/USB/proxmox-ve_9.2-1.iso", Size: isoSize}}
	}
	m = New(m.deps, Options{})
	m = toDeviceScreen(t, m)
	if ok, _ := m.targets(); len(ok) != 0 {
		t.Fatalf("the stick holding the image must not be offered, got %+v", ok)
	}
	if !strings.Contains(m.View(), "contains the selected image") {
		t.Fatal("the device screen must explain why the stick is unavailable")
	}
}

func TestConfirmRequiresTypingYes(t *testing.T) {
	fb := &fakeBurn{jobs: make(chan burn.Job, 1)}
	m := toConfirmScreen(t, newModel(t, []device.Device{stick}, fb))

	m = typeText(t, m, "ye")
	m, cmd := update(t, m, key("enter"))
	if m.screen != screenConfirm || cmd != nil {
		t.Fatal(`writing must not start until "yes" is typed`)
	}
	if !strings.Contains(m.View(), "Type yes") {
		t.Fatal("the confirm screen must explain what to type")
	}

	m = typeText(t, m, "s")
	m, cmd = update(t, m, key("enter"))
	if m.screen != screenWriting || cmd == nil {
		t.Fatalf("screen = %v, want writing after typing yes", m.screen)
	}
	cmds := collect(cmd)
	go func() {
		for _, c := range cmds {
			go c()
		}
	}()
	job := <-fb.jobs
	if job.Device.Path != "/dev/disk6" || job.Image != "/Users/dev/Downloads/proxmox-ve_9.2-1.iso" || !job.Verify {
		t.Fatalf("job = %+v, want the ISO on /dev/disk6 with verification", job)
	}
}

func TestTabTogglesVerification(t *testing.T) {
	m := toConfirmScreen(t, newModel(t, []device.Device{stick}, &fakeBurn{}))
	if !m.verify {
		t.Fatal("verification must be on by default")
	}
	m, _ = update(t, m, key("tab"))
	if m.verify {
		t.Fatal("tab must turn verification off")
	}
}

func TestEscapeGoesBackWithoutWriting(t *testing.T) {
	m := toConfirmScreen(t, newModel(t, []device.Device{stick}, &fakeBurn{}))
	m, _ = update(t, m, key("esc"))
	if m.screen != screenDevice {
		t.Fatalf("esc on confirm = %v, want the device screen", m.screen)
	}
	m, _ = update(t, m, key("esc"))
	if m.screen != screenImage {
		t.Fatalf("esc on devices = %v, want the image screen", m.screen)
	}
}

func TestTypedPathIsValidated(t *testing.T) {
	m := newModel(t, []device.Device{stick}, &fakeBurn{})
	m, _ = update(t, m, key("down")) // "Type a path…"
	m, _ = update(t, m, key("enter"))
	if m.screen != screenPath {
		t.Fatalf("screen = %v, want the path screen", m.screen)
	}
	m = typeText(t, m, "~/missing.iso")
	m, _ = update(t, m, key("enter"))
	if m.screen != screenPath || !strings.Contains(m.View(), "does not exist") {
		t.Fatalf("a missing file must keep the path screen with an error:\n%s", m.View())
	}
	m.pathInput.SetValue("~/Downloads/proxmox-ve_9.2-1.iso")
	m, _ = update(t, m, key("enter"))
	if want := filepath.Join("/Users/dev", "Downloads", "proxmox-ve_9.2-1.iso"); m.screen != screenDevice || m.image.Path != want {
		t.Fatalf("screen = %v image = %q, want the device screen with the expanded path", m.screen, m.image.Path)
	}
}

func TestSuccessfulWriteShowsTheResult(t *testing.T) {
	fb := &fakeBurn{jobs: make(chan burn.Job, 1)}
	m := toConfirmScreen(t, newModel(t, []device.Device{stick}, fb))
	m = typeText(t, m, "yes")
	m, cmd := update(t, m, key("enter"))

	cmds := collect(cmd) // [run burn, wait for events]
	go cmds[0]()
	wait := cmds[1]
	for m.screen == screenWriting {
		m, wait = update(t, m, wait())
		if wait == nil && m.screen == screenWriting {
			t.Fatal("writing screen stopped listening for progress")
		}
	}
	if m.screen != screenDone {
		t.Fatalf("screen = %v, want done; err = %v", m.screen, m.err)
	}
	view := m.View()
	for _, want := range []string{"Done", "abc123", "1m12s"} {
		if !strings.Contains(view, want) {
			t.Errorf("done screen is missing %q:\n%s", want, view)
		}
	}
}

func TestCancellingAWriteNeedsTwoCtrlC(t *testing.T) {
	fb := &fakeBurn{jobs: make(chan burn.Job, 1), block: true}
	m := toConfirmScreen(t, newModel(t, []device.Device{stick}, fb))
	m = typeText(t, m, "yes")
	m, cmd := update(t, m, key("enter"))
	cmds := collect(cmd)
	go cmds[0]()
	<-fb.jobs
	m, wait := update(t, m, cmds[1]()) // first progress report

	m, quit := update(t, m, key("ctrl+c"))
	if quit != nil || m.screen != screenWriting || m.cancelling {
		t.Fatal("the first ctrl+c must only warn, not stop or quit")
	}
	if !strings.Contains(m.View(), "ctrl+c again") {
		t.Fatalf("the writing screen must warn before cancelling:\n%s", m.View())
	}

	m, _ = update(t, m, key("ctrl+c"))
	if !m.cancelling {
		t.Fatal("the second ctrl+c must cancel the write")
	}
	for m.screen == screenWriting {
		m, wait = update(t, m, wait())
	}
	if m.screen != screenFailed || !m.cancelled || !strings.Contains(m.View(), "cancelled") {
		t.Fatalf("screen = %v cancelled = %v, want a cancelled result:\n%s", m.screen, m.cancelled, m.View())
	}
}

func TestSignalsDuringAWriteBecomeACancelRequest(t *testing.T) {
	m := newModel(t, []device.Device{stick}, &fakeBurn{})
	m.screen = screenWriting
	for _, sig := range []tea.Msg{tea.QuitMsg{}, tea.InterruptMsg{}} {
		got, ok := Filter(m, sig).(tea.KeyMsg)
		if !ok || got.Type != tea.KeyCtrlC {
			t.Fatalf("Filter(%T) during a write = %#v, want a ctrl+c key (two are needed to cancel)", sig, got)
		}
	}
	m.screen = screenDevice
	if _, ok := Filter(m, tea.QuitMsg{}).(tea.QuitMsg); !ok {
		t.Fatal("outside a write, quitting must still work")
	}
}
