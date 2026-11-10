package privilege

import (
	"flag"
	"testing"
)

// Go's flag parser stops at the first positional argument, so the elevated
// marker must come before the image path or it is never recognized.
func TestRelaunchArgsPutTheFlagBeforeTheImage(t *testing.T) {
	image := `D:\isos\proxmox ve.iso`
	flags := flag.NewFlagSet("create-bootable-usb", flag.ContinueOnError)
	elevated := flags.Bool(ElevatedFlag[2:], false, "")
	if err := flags.Parse(RelaunchArgs([]string{image})); err != nil {
		t.Fatal(err)
	}
	if !*elevated {
		t.Fatal("the relaunched process must see --elevated")
	}
	if flags.NArg() != 1 || flags.Arg(0) != image {
		t.Fatalf("positional args = %v, want only the image", flags.Args())
	}
}
