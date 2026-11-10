# create-bootable-usb

Write an ISO or IMG image (Proxmox, Debian, Ubuntu, Raspberry Pi OS…) to a USB stick from a friendly terminal interface instead of a raw `dd` command. It runs on **macOS, Linux and Windows**. It only offers removable USB and SD devices, asks you to type `yes` before it erases anything, and reads every byte back to verify the write.

```
 create-bootable-usb  v0.0.1
✓ Image  ›  ● Device  ›  ○ Confirm  ›  ○ Write

Image proxmox-ve_9.2-1.iso · 1.7 GB

Which device do you want to erase and write the image to?

› /dev/disk6  ProductCode · 16.0 GB · USB · "USB"

Not available:
  /dev/disk4  KINGSTON SNV3S2000G · 2.0 TB · PCI-Express · "ExternalSSD"
              ↳ not a removable or USB device

↑/↓ move • enter select • r rescan • esc back • q quit
```

## Quick start

### With Homebrew (macOS and Linux)

This repository is also a Homebrew tap:

```sh
brew tap elvisgastelum/create-bootable-usb https://github.com/elvisgastelum/create-bootable-usb
brew install elvisgastelum/create-bootable-usb/create-bootable-usb
create-bootable-usb
```

Install it by its full name the first time: Homebrew only loads formulae from a third-party tap you have trusted, and naming the tap in `brew install` trusts it (a plain `brew install create-bootable-usb` stops with "Refusing to load formula … from untrusted tap"). After that the short name works.

The formula installs the same release binaries as the downloads below (Apple Silicon, Intel, Linux x86_64 and ARM64) and is updated on every release. Update with `brew upgrade create-bootable-usb`. Remove it with `brew uninstall create-bootable-usb && brew untap elvisgastelum/create-bootable-usb`.

### Without Homebrew

1. **Download** the file for your computer from the [latest release](https://github.com/elvisgastelum/create-bootable-usb/releases/latest). You do not need Go installed.

   | Computer | File |
   |----------|------|
   | macOS, Apple Silicon (M1–M4, ARM) | `create-bootable-usb_macos_arm64.tar.gz` |
   | macOS, Intel (x86_64) | `create-bootable-usb_macos_x86_64.tar.gz` |
   | Linux, x86_64 | `create-bootable-usb_linux_x86_64.tar.gz` |
   | Linux, ARM64 (Raspberry Pi 4/5, ARM servers) | `create-bootable-usb_linux_arm64.tar.gz` |
   | Windows, x86_64 | `create-bootable-usb_windows_x86_64.zip` |

2. **Unpack and run it.**

   macOS and Linux (change the file name to match your computer):

   ```sh
   curl -fsSL https://github.com/elvisgastelum/create-bootable-usb/releases/latest/download/create-bootable-usb_macos_arm64.tar.gz | tar -xzf - create-bootable-usb
   ./create-bootable-usb
   ```

   Windows: unzip the file and double-click `create-bootable-usb.exe`, or run it from PowerShell.

3. **Follow the screens:** pick the image, pick the USB device, type `yes`. When it says **Done**, unplug the stick and boot from it.

> [!NOTE]
> Writing a raw device needs administrator rights. On macOS and Linux the program restarts itself through `sudo` and asks for your password. On Windows it shows the UAC prompt and continues in a new window.

### Example: a Proxmox VE installer

```sh
./create-bootable-usb ~/Downloads/proxmox-ve_9.2-1.iso
```

Passing the image skips the first screen. Choose the stick (for example `/dev/disk6 · 16.0 GB · USB · "USB"`), type `yes`, and wait for the write and verification to finish.

## What it does for you

| Step | What happens |
|------|--------------|
| Image | Lists the `.iso` and `.img` files in your Downloads and Desktop folders and the current folder, newest first. You can also type, paste or drag in a path. |
| Device | Lists removable and external disks every 3 seconds, so a stick plugged in later appears on its own. Devices it will not write are shown with the reason. |
| Confirm | Shows the image, the device, its volumes and what will be unmounted. Nothing happens until you type `yes`. Press `tab` to turn verification off or on. |
| Write | Checks the device again, unmounts it, writes in 4 MiB chunks (the partition table last, so no OS mounts a half-written stick), flushes, reads everything back and compares it, then ejects (macOS). Shows speed and time left. |
| Done | Prints the image's SHA-256 so you can compare it with the checksum on the download page. |

## Safety rules

The program refuses to write when:

- the disk holds the running operating system (the boot disk, or a live USB you booted from);
- the disk is internal or not removable: internal SSDs and eMMC storage never appear, and external Thunderbolt/NVMe drives are never offered (macOS lists them as unavailable);
- the image does not fit on the device;
- the image file is stored on the device you are about to erase;
- the device was unplugged, replaced or renumbered between selecting it and writing. It is checked again right before writing.

Cancelling a write takes two presses of `ctrl+c`. A cancelled stick will not boot until it is written again.

## Command line

```text
create-bootable-usb [image]          start the interactive writer
create-bootable-usb --list [image]   list detected devices and exit
create-bootable-usb --version        print the version and exit
```

`--list` needs no admin rights on macOS and Linux. With an image path it also tells you whether each device is big enough:

```text
DEVICE       DESCRIPTION                                                  MOUNTED                  STATUS
/dev/disk4   KINGSTON SNV3S2000G · 2.0 TB · PCI-Express · "ExternalSSD"   [/Volumes/ExternalSSD]   unavailable: not a removable or USB device
/dev/disk6   ProductCode · 16.0 GB · USB · "USB"                          [/Volumes/USB]           ready
```

## Platform notes

| | macOS | Linux | Windows |
|---|---|---|---|
| Finds devices with | `diskutil` | `lsblk` (util-linux 2.27+) | PowerShell `Get-Disk` |
| Before writing | `diskutil unmountDisk` (APFS containers too) | `umount` each mounted partition | `diskpart clean` (removes the partitions) |
| Writes to | `/dev/rdiskN` (raw, fast) | `/dev/sdX`, opened exclusively | `\\.\PhysicalDriveN` |
| Admin rights | `sudo` | `sudo` | UAC prompt |

- **macOS, downloaded with a browser:** if macOS says the app "cannot be opened", remove the quarantine flag with `xattr -d com.apple.quarantine ./create-bootable-usb`. Downloads made with `curl` are not affected.
- **macOS after writing:** a dialog may say the disk is not readable. Click **Eject**. That is normal for Linux installer images.
- **Windows installer ISOs** are not "hybrid" images and do not boot when written byte for byte. The program warns you about such images. Linux installers are hybrid and work.

## Build from source

You need Go 1.26 or newer.

```sh
git clone https://github.com/elvisgastelum/create-bootable-usb.git
cd create-bootable-usb
make build        # bin/create-bootable-usb
make run          # build and start (or: go run .)
```

Or install it directly with `go install github.com/elvisgastelum/create-bootable-usb@latest`.

| Make target | What it does |
|-------------|--------------|
| `make build` | Build for this computer into `bin/` |
| `make run ARGS=~/Downloads/x.iso` | Build and start the writer |
| `make list` | Build and list detected devices |
| `make test` | Run the unit tests |
| `make test-integration` | macOS: write and verify a RAM disk through `/dev/rdisk` (no USB device needed) |
| `make vet` | Vet the code for every release platform |
| `make cross` | Build all five release binaries into `dist/` |
| `make snapshot` | Build the release archives locally with GoReleaser |
| `make formula V=0.0.2` | Regenerate `Formula/create-bootable-usb.rb` from `dist/checksums.txt` (the release workflow does this on every release) |
| `make release V=v0.0.2` | Tag and push a version; GitHub Actions builds and publishes it |
| `make clean` | Remove `bin/` and `dist/` |

### Project layout

| Path | Responsibility |
|------|----------------|
| `main.go` | Flags, `--list`, privilege elevation, starts the interface |
| `internal/tui` | The screens (Bubble Tea) |
| `internal/device` | Finding devices and preparing them, one file per OS |
| `internal/burn` | One write from start to finish, with all safety checks |
| `internal/writer` | Sector-aligned copy and byte-for-byte verification |
| `internal/images` | Finding and inspecting `.iso`/`.img` files |
| `internal/privilege` | `sudo` / UAC elevation |
| `Formula/` | The Homebrew formula (this repository is a tap) |
| `tools/brewformula` | Generates the formula from a release's `checksums.txt` |

## Doing it by hand with `dd`

The program automates these steps. If you ever need them without it, here is the macOS version:

```sh
diskutil list external                     # find the stick, e.g. /dev/disk6 — double-check the size!
diskutil unmountDisk /dev/disk6
sudo dd if=~/Downloads/proxmox-ve_9.2-1.iso of=/dev/rdisk6 bs=4m status=progress
diskutil eject /dev/disk6
```

On Linux the target is the whole disk (`/dev/sdX`, not `/dev/sdX1`). Use `bs=4M conv=fsync` and run `sync` afterwards. A typo in `of=` erases the wrong disk without asking, which is the mistake this tool is built to prevent.

## License

[MIT](LICENSE)
