package device

import (
	"bytes"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"
)

// windowsDiskScript lists every disk with its bus, boot/system flags,
// volume labels and drive letters as a JSON array.
const windowsDiskScript = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.Encoding]::UTF8
$out = @()
foreach ($d in Get-Disk) {
  $labels = @()
  $letters = @()
  foreach ($p in @(Get-Partition -DiskNumber $d.Number -ErrorAction SilentlyContinue)) {
    if ([string]$p.DriveLetter -match '^[A-Za-z]$') { $letters += [string]$p.DriveLetter }
    $v = $p | Get-Volume -ErrorAction SilentlyContinue
    if ($v -and $v.FileSystemLabel) { $labels += [string]$v.FileSystemLabel }
  }
  $out += [pscustomobject]@{
    Number       = [int]$d.Number
    FriendlyName = [string]$d.FriendlyName
    Size         = [int64]$d.Size
    BusType      = [string]$d.BusType
    IsBoot       = [bool]$d.IsBoot
    IsSystem     = [bool]$d.IsSystem
    Labels       = $labels
    Letters      = $letters
  }
}
ConvertTo-Json -InputObject @($out) -Depth 3 -Compress
`

type windowsDisk struct {
	Number       int        `json:"Number"`
	FriendlyName string     `json:"FriendlyName"`
	Size         flexInt    `json:"Size"`
	BusType      string     `json:"BusType"`
	IsBoot       bool       `json:"IsBoot"`
	IsSystem     bool       `json:"IsSystem"`
	Labels       stringList `json:"Labels"`
	Letters      stringList `json:"Letters"`
}

// parseWindowsDisks turns the JSON printed by windowsDiskScript into
// devices, keeping only disks on a USB, SD or MMC bus.
func parseWindowsDisks(data []byte) ([]Device, error) {
	data = bytes.TrimSpace(bytes.TrimPrefix(data, []byte("\xef\xbb\xbf")))
	if len(data) == 0 || bytes.Equal(data, []byte("null")) {
		return nil, nil
	}
	var disks []windowsDisk
	if data[0] == '{' {
		var one windowsDisk
		if err := json.Unmarshal(data, &one); err != nil {
			return nil, fmt.Errorf("parse disk list: %w", err)
		}
		disks = []windowsDisk{one}
	} else if err := json.Unmarshal(data, &disks); err != nil {
		return nil, fmt.Errorf("parse disk list: %w", err)
	}

	var devices []Device
	for _, w := range disks {
		bus := strings.TrimSpace(w.BusType)
		if !removableBus(bus) {
			continue
		}
		id := fmt.Sprintf("PhysicalDrive%d", w.Number)
		d := Device{
			ID:      id,
			Path:    `\\.\` + id,
			RawPath: `\\.\` + id,
			Number:  w.Number,
			Model:   strings.TrimSpace(w.FriendlyName),
			Size:    int64(w.Size),
			Bus:     bus,
			System:  w.IsBoot || w.IsSystem,
			Labels:  []string(w.Labels),
		}
		for _, letter := range w.Letters {
			d.Mounts = append(d.Mounts, letter+":")
		}
		devices = append(devices, d)
	}
	return devices, nil
}

// encodePowerShell encodes a script for powershell.exe -EncodedCommand
// (base64 of UTF-16LE), which avoids every command-line quoting problem.
func encodePowerShell(script string) string {
	units := utf16.Encode([]rune(script))
	buf := make([]byte, 2*len(units))
	for i, u := range units {
		binary.LittleEndian.PutUint16(buf[2*i:], u)
	}
	return base64.StdEncoding.EncodeToString(buf)
}
