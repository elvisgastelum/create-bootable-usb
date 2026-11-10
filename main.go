// Command create-bootable-usb writes an ISO or IMG image to a USB device
// through an interactive terminal interface, on macOS, Linux and Windows.
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"text/tabwriter"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/elvisgastelum/create-bootable-usb/internal/burn"
	"github.com/elvisgastelum/create-bootable-usb/internal/device"
	"github.com/elvisgastelum/create-bootable-usb/internal/images"
	"github.com/elvisgastelum/create-bootable-usb/internal/privilege"
	"github.com/elvisgastelum/create-bootable-usb/internal/tui"
)

// Set by the release build (-ldflags "-X main.version=...").
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

const usageText = `create-bootable-usb writes an ISO or IMG image to a USB device.

Usage:
  create-bootable-usb [image]          start the interactive writer
  create-bootable-usb --list [image]   list detected devices and exit
  create-bootable-usb --version        print the version and exit

Writing needs administrator rights: on macOS and Linux the program asks for
your password through sudo; on Windows it asks through a UAC prompt.

Flags:
`

func main() { os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr)) }

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	flags := flag.NewFlagSet("create-bootable-usb", flag.ContinueOnError)
	flags.SetOutput(stderr)
	list := flags.Bool("list", false, "list detected devices and exit")
	showVersion := flags.Bool("version", false, "print the version and exit")
	elevated := flags.Bool(privilege.ElevatedFlag[2:], false, "internal: set on the relaunched Windows process")
	flags.Usage = func() {
		fmt.Fprint(stderr, usageText)
		flags.VisitAll(func(f *flag.Flag) {
			if f.Name != privilege.ElevatedFlag[2:] {
				fmt.Fprintf(stderr, "  --%-10s %s\n", f.Name, f.Usage)
			}
		})
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 2
	}
	if flags.NArg() > 1 {
		flags.Usage()
		return 2
	}

	// fail prints an error; a relaunched Windows window would close before
	// it could be read, so it waits for Enter there.
	fail := func(format string, a ...any) int {
		fmt.Fprintf(stderr, "error: "+format+"\n", a...)
		if *elevated {
			fmt.Fprint(stderr, "Press Enter to close this window.")
			bufio.NewReader(stdin).ReadString('\n')
		}
		return 1
	}

	if *showVersion {
		fmt.Fprintf(stdout, "create-bootable-usb %s (commit %s, built %s, %s/%s)\n", version, commit, date, runtime.GOOS, runtime.GOARCH)
		return 0
	}

	home := images.HomeDir()
	var image images.Info
	if flags.NArg() == 1 {
		path, err := filepath.Abs(images.Expand(flags.Arg(0), home))
		if err != nil {
			return fail("%v", err)
		}
		if image, err = images.Inspect(path); err != nil {
			return fail("%v", err)
		}
	}

	if *list {
		return printDevices(stdout, stderr, image)
	}

	if !privilege.IsElevated() {
		fmt.Fprintln(stdout, "Writing to USB devices needs administrator rights; asking for them now…")
		// Pass the absolute image path: the elevated process may start in
		// another folder (Windows) and the user's flags are not needed again.
		var relaunch []string
		if image.Path != "" {
			relaunch = []string{image.Path}
		}
		err := privilege.Elevate(relaunch)
		if errors.Is(err, privilege.ErrRelaunched) {
			fmt.Fprintln(stdout, "Continuing in a new Administrator window.")
			return 0
		}
		return fail("%v\nRun it again as an administrator (sudo create-bootable-usb, or \"Run as administrator\" on Windows).", err)
	}

	host := device.Host{}
	model := tui.New(tui.Deps{
		Version:     version,
		Home:        home,
		ListDevices: device.List,
		FindImages:  func() []images.Candidate { return images.Discover(images.DefaultDirs(home)) },
		Inspect:     images.Inspect,
		Burn: func(ctx context.Context, job burn.Job, report func(burn.Progress)) (burn.Result, error) {
			return burn.Run(ctx, host, job, report)
		},
	}, tui.Options{Image: image.Path})
	if _, err := tea.NewProgram(model, tea.WithFilter(tui.Filter)).Run(); err != nil {
		return fail("%v", err)
	}
	return 0
}

// printDevices lists every detected device and whether it can be used.
func printDevices(stdout, stderr io.Writer, image images.Info) int {
	devices, err := device.List()
	if err != nil {
		fmt.Fprintf(stderr, "error: could not list devices: %v\n", err)
		return 1
	}
	if len(devices) == 0 {
		fmt.Fprintln(stdout, "No removable or external devices found.")
		return 0
	}
	w := tabwriter.NewWriter(stdout, 0, 0, 3, ' ', 0)
	fmt.Fprintln(w, "DEVICE\tDESCRIPTION\tMOUNTED\tSTATUS")
	for _, d := range devices {
		status := "ready"
		if problem := d.Problem(image.Size); problem != "" {
			status = "unavailable: " + problem
		} else if image.Path != "" && burn.ImageOnDevice(image.Path, d) {
			status = "unavailable: contains the image"
		}
		mounted := "-"
		if len(d.Mounts) > 0 {
			mounted = fmt.Sprint(d.Mounts)
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", d.Path, d.Summary(), mounted, status)
	}
	if err := w.Flush(); err != nil {
		return 1
	}
	return 0
}
