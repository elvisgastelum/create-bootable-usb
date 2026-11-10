package tui

import (
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"

	"github.com/elvisgastelum/create-bootable-usb/internal/burn"
	"github.com/elvisgastelum/create-bootable-usb/internal/format"
	"github.com/elvisgastelum/create-bootable-usb/internal/images"
)

var (
	accent      = lipgloss.AdaptiveColor{Light: "#5A3FD6", Dark: "#9D8CFF"}
	accentStyle = lipgloss.NewStyle().Foreground(accent)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#FFFFFF")).Background(lipgloss.Color("#5A3FD6")).Padding(0, 1)
	boldStyle   = lipgloss.NewStyle().Bold(true)
	selStyle    = lipgloss.NewStyle().Bold(true).Foreground(accent)
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#7A7A7A", Dark: "#8A8A8A"})
	okStyle     = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#1A7F37", Dark: "#3FB950"})
	warnStyle   = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#9A6700", Dark: "#D29922"})
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"})
	dangerBox   = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}).
			Border(lipgloss.RoundedBorder()).BorderForeground(lipgloss.AdaptiveColor{Light: "#CF222E", Dark: "#F85149"}).Padding(0, 1)
	labelStyle = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#7A7A7A", Dark: "#8A8A8A"}).Width(10)
)

// View renders the current screen.
func (m Model) View() string {
	var body string
	switch m.screen {
	case screenImage:
		body = m.viewImage()
	case screenPath:
		body = m.viewPath()
	case screenDevice:
		body = m.viewDevice()
	case screenConfirm:
		body = m.viewConfirm()
	case screenWriting:
		body = m.viewWriting()
	case screenDone:
		body = m.viewDone()
	case screenFailed:
		body = m.viewFailed()
	}
	return m.header() + "\n\n" + body + "\n"
}

func (m Model) contentWidth() int { return max(40, min(m.width, 88)) - 2 }

func (m Model) wrap(s string) string { return lipgloss.NewStyle().Width(m.contentWidth()).Render(s) }

func (m Model) header() string {
	title := titleStyle.Render("create-bootable-usb") + " " + dimStyle.Render(m.deps.Version)
	current := map[screen]int{screenImage: 0, screenPath: 0, screenDevice: 1, screenConfirm: 2,
		screenWriting: 3, screenDone: 3, screenFailed: 3}[m.screen]
	var steps []string
	for i, name := range []string{"Image", "Device", "Confirm", "Write"} {
		switch {
		case i < current || m.screen == screenDone:
			steps = append(steps, okStyle.Render("✓ "+name))
		case i == current:
			steps = append(steps, selStyle.Render("● "+name))
		default:
			steps = append(steps, dimStyle.Render("○ "+name))
		}
	}
	return title + "\n" + strings.Join(steps, dimStyle.Render("  ›  "))
}

func help(keys ...string) string { return dimStyle.Render(strings.Join(keys, " • ")) }

func pad(s string, width int) string {
	return s + strings.Repeat(" ", max(0, width-lipgloss.Width(s)))
}

func cursor(selected bool, s string) string {
	if selected {
		return selStyle.Render("› " + s)
	}
	return "  " + s
}

func (m Model) viewImage() string {
	var b strings.Builder
	b.WriteString(boldStyle.Render("Which image do you want to write?") + "\n\n")
	if len(m.candidates) == 0 {
		b.WriteString(dimStyle.Render("No .iso or .img files were found in your Downloads or Desktop folders\nor in the current folder.") + "\n\n")
	}
	nameWidth := 0
	for _, c := range m.candidates {
		nameWidth = max(nameWidth, lipgloss.Width(filepath.Base(c.Path)))
	}
	for i, c := range m.candidates {
		line := pad(filepath.Base(c.Path), nameWidth) + "  " + pad(format.Bytes(c.Size), 9)
		meta := dimStyle.Render(images.Shorten(filepath.Dir(c.Path), m.deps.Home) + " · " + ago(c.Modified))
		b.WriteString(cursor(i == m.imageCursor, line) + " " + meta + "\n")
	}
	b.WriteString(cursor(m.imageCursor == len(m.candidates), "Type a path…") + "\n")
	if m.imageErr != "" {
		b.WriteString("\n" + errStyle.Render(m.wrap("✗ "+m.imageErr)) + "\n")
	}
	b.WriteString("\n" + help("↑/↓ move", "enter select", "q quit"))
	return b.String()
}

func (m Model) viewPath() string {
	var b strings.Builder
	b.WriteString(boldStyle.Render("Type or paste the path of the image") + "\n")
	b.WriteString(dimStyle.Render("You can also drag the file into this window. ~ means your home folder.") + "\n\n")
	b.WriteString(m.pathInput.View() + "\n")
	if m.pathErr != "" {
		b.WriteString("\n" + errStyle.Render(m.wrap("✗ "+m.pathErr)) + "\n")
	}
	b.WriteString("\n" + help("enter continue", "esc back", "ctrl+c quit"))
	return b.String()
}

func (m Model) imageLine() string {
	return dimStyle.Render("Image ") + filepath.Base(m.image.Path) + dimStyle.Render(" · "+format.Bytes(m.image.Size))
}

func (m Model) hybridWarning() string {
	if m.image.Hybrid || m.image.Size == 0 {
		return ""
	}
	return warnStyle.Render(m.wrap("⚠ This image has no MBR boot signature, so it may not boot when written as-is. "+
		"Linux installers (Proxmox, Debian, Ubuntu…) are fine; Windows installer ISOs need a dedicated tool.")) + "\n\n"
}

func (m Model) viewDevice() string {
	var b strings.Builder
	b.WriteString(m.imageLine() + "\n\n")
	b.WriteString(m.hybridWarning())
	b.WriteString(boldStyle.Render("Which device do you want to erase and write the image to?") + "\n\n")

	ok, blocked := m.targets()
	pathWidth := 0
	for _, d := range m.devices {
		pathWidth = max(pathWidth, lipgloss.Width(d.Path))
	}
	switch {
	case m.devices == nil && m.scanning:
		b.WriteString(m.spinner.View() + " Looking for USB devices…\n")
	case m.scanErr != "" && m.devices == nil:
		// the error is shown below
	case len(ok) == 0:
		b.WriteString(warnStyle.Render("No usable USB device found.") + dimStyle.Render(" Plug one in; this list refreshes on its own.") + "\n")
	}
	for i, d := range ok {
		b.WriteString(cursor(i == m.deviceCursor, pad(d.Path, pathWidth)+"  "+d.Summary()) + "\n")
	}
	if len(blocked) > 0 {
		b.WriteString("\n" + dimStyle.Render("Not available:") + "\n")
		for _, x := range blocked {
			b.WriteString(dimStyle.Render("  "+pad(x.device.Path, pathWidth)+"  "+x.device.Summary()) + "\n")
			b.WriteString(dimStyle.Render("  "+strings.Repeat(" ", pathWidth)+"  ↳ "+x.reason) + "\n")
		}
	}
	if m.scanErr != "" {
		b.WriteString("\n" + errStyle.Render(m.wrap("✗ Could not list devices: "+m.scanErr)) + "\n")
	}
	scan := "r rescan"
	if m.scanning && m.devices != nil {
		scan = m.spinner.View() + " scanning"
	}
	b.WriteString("\n" + help("↑/↓ move", "enter select", scan, "esc back", "q quit"))
	return b.String()
}

func (m Model) viewConfirm() string {
	var b strings.Builder
	d := m.selected
	b.WriteString(dangerBox.Render("⚠  Everything on "+d.Path+" will be erased") + "\n\n")
	b.WriteString(labelStyle.Render("Image") + filepath.Base(m.image.Path) + dimStyle.Render(" · "+format.Bytes(m.image.Size)) + "\n")
	b.WriteString(labelStyle.Render("") + dimStyle.Render(images.Shorten(m.image.Path, m.deps.Home)) + "\n")
	b.WriteString(labelStyle.Render("Device") + boldStyle.Render(d.Path) + "\n")
	b.WriteString(labelStyle.Render("") + d.Summary() + "\n")
	if len(d.Mounts) > 0 {
		b.WriteString(labelStyle.Render("Mounted") + strings.Join(d.Mounts, ", ") + dimStyle.Render("  (will be unmounted)") + "\n")
	}
	check := "[ ]"
	if m.verify {
		check = okStyle.Render("[x]")
	}
	b.WriteString(labelStyle.Render("Verify") + check + " read the device back after writing" + dimStyle.Render("  (tab toggles)") + "\n\n")
	b.WriteString(m.hybridWarning())
	b.WriteString("Type " + boldStyle.Render("yes") + " to erase " + boldStyle.Render(d.Path) + " and write the image:\n")
	b.WriteString(m.confirmInput.View() + "\n")
	if m.confirmErr != "" {
		b.WriteString(errStyle.Render(m.confirmErr) + "\n")
	}
	b.WriteString("\n" + help("enter start", "tab verify on/off", "esc back", "ctrl+c quit"))
	return b.String()
}

type stageRow struct {
	stage burn.Stage
	label string
}

func (m Model) stageRows() []stageRow {
	finish := "Finish"
	if runtime.GOOS == "darwin" {
		finish = "Eject"
	}
	rows := []stageRow{
		{burn.StagePreparing, "Check and unmount the device"},
		{burn.StageWriting, "Write the image"},
		{burn.StageFlushing, "Flush to the device"},
	}
	if m.verify {
		rows = append(rows, stageRow{burn.StageVerifying, "Verify"})
	}
	return append(rows, stageRow{burn.StageFinishing, finish})
}

func (m Model) viewWriting() string {
	var b strings.Builder
	b.WriteString(boldStyle.Render("Writing "+filepath.Base(m.image.Path)+" → "+m.selected.Path) + "\n\n")
	for _, row := range m.stageRows() {
		switch {
		case row.stage < m.stage:
			b.WriteString(okStyle.Render("  ✓ "+row.label) + "\n")
		case row.stage == m.stage:
			b.WriteString("  " + m.spinner.View() + boldStyle.Render(row.label) + "\n")
			if m.total > 0 {
				b.WriteString(m.progressLines())
			}
		default:
			b.WriteString(dimStyle.Render("  ○ "+row.label) + "\n")
		}
	}
	b.WriteString("\n")
	switch {
	case m.cancelling:
		b.WriteString(warnStyle.Render("Stopping…") + "\n\n")
	case m.cancelArmed:
		b.WriteString(warnStyle.Render(m.wrap("⚠ Press ctrl+c again to stop. The device will not boot until it is written again. Press esc to keep going.")) + "\n\n")
	}
	b.WriteString(help("do not unplug the device", "ctrl+c cancel"))
	return b.String()
}

func (m Model) progressLines() string {
	pct := float64(m.done) / float64(m.total)
	m.bar.Width = max(20, min(m.contentWidth()-14, 50))
	elapsed := time.Since(m.stageStart).Seconds()
	speed, eta := 0.0, ""
	if elapsed > 0.5 && m.done > 0 {
		speed = float64(m.done) / elapsed
		eta = " · about " + format.Duration(time.Duration(float64(m.total-m.done)/speed*float64(time.Second))) + " left"
	}
	return fmt.Sprintf("      %s %3.0f%%\n      %s\n", m.bar.ViewAs(pct), pct*100,
		dimStyle.Render(format.Bytes(m.done)+" of "+format.Bytes(m.total)+" · "+format.Rate(speed)+eta))
}

func (m Model) viewDone() string {
	var b strings.Builder
	r := m.result
	b.WriteString(okStyle.Bold(true).Render("✓ Done! "+m.selected.Path+" is ready to boot.") + "\n\n")
	b.WriteString(labelStyle.Render("Image") + filepath.Base(m.image.Path) + "\n")
	b.WriteString(labelStyle.Render("Device") + m.selected.Path + dimStyle.Render(" · "+m.selected.Name()) + "\n")
	if r.Verified {
		b.WriteString(labelStyle.Render("Verified") + okStyle.Render("every byte matches the image") + "\n")
	} else {
		b.WriteString(labelStyle.Render("Verified") + dimStyle.Render("skipped") + "\n")
	}
	b.WriteString(labelStyle.Render("SHA-256") + r.SHA256 + "\n")
	b.WriteString(labelStyle.Render("Time") + format.Duration(r.Duration) + "\n\n")
	b.WriteString(dimStyle.Render(m.wrap("Compare the SHA-256 with the checksum published by the image's website to be sure the download was not damaged.")) + "\n\n")
	switch {
	case r.Warning != "":
		b.WriteString(warnStyle.Render(m.wrap("⚠ "+r.Warning+". Eject the device yourself before unplugging it.")) + "\n\n")
	case runtime.GOOS == "darwin":
		b.WriteString(m.wrap("The device was ejected; you can unplug it. If macOS says the disk is not readable, click Eject: that is normal for Linux installers.") + "\n\n")
	default:
		b.WriteString("You can unplug the device now.\n\n")
	}
	b.WriteString(help("enter quit", "n write to another device"))
	return b.String()
}

func (m Model) viewFailed() string {
	var b strings.Builder
	if m.cancelled {
		b.WriteString(warnStyle.Bold(true).Render("Writing was cancelled.") + "\n\n")
		b.WriteString(m.wrap(m.selected.Path+" was only partly written and will not boot. Write it again, or reformat it to use it for files.") + "\n\n")
	} else {
		b.WriteString(errStyle.Bold(true).Render("✗ The image could not be written.") + "\n\n")
		b.WriteString(errStyle.Render(m.wrap(m.err.Error())) + "\n\n")
		if h := hint(m.err.Error()); h != "" {
			b.WriteString(m.wrap(h) + "\n\n")
		}
	}
	b.WriteString(help("enter choose a device again", "q quit"))
	return b.String()
}

// hint suggests a next step for common failures.
func hint(msg string) string {
	s := strings.ToLower(msg)
	switch {
	case strings.Contains(s, "permission denied"), strings.Contains(s, "access is denied"), strings.Contains(s, "operation not permitted"):
		return "Run the program with administrator rights: sudo on macOS and Linux, \"Run as administrator\" on Windows."
	case strings.Contains(s, "busy"), strings.Contains(s, "in use"):
		return "Close any window or program that is using the device (Finder, Explorer, a file manager), then try again."
	case strings.Contains(s, "no longer connected"), strings.Contains(s, "changed since"):
		return "Check that the device is plugged in, then select it again."
	case strings.Contains(s, "verification failed"):
		return "Try again, preferably on another USB port. If it keeps failing, the device may be worn out or counterfeit."
	case strings.Contains(s, "stored on"):
		return "Copy the image to your computer first, then write it."
	}
	return ""
}

func ago(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	d := time.Since(t)
	switch {
	case d < time.Hour:
		return "just now"
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 48*time.Hour:
		return "yesterday"
	default:
		return t.Format("Jan 2, 2006")
	}
}
