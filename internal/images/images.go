// Package images finds and inspects disk images (.iso, .img) on this computer.
package images

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

// MaxCandidates caps how many discovered images are offered.
const MaxCandidates = 20

// Candidate is an image file found in one of the scanned folders.
type Candidate struct {
	Path     string
	Size     int64
	Modified time.Time
}

// Info describes an image chosen for writing.
type Info struct {
	Path string
	Size int64
	// Hybrid reports an MBR boot signature at bytes 510-511. Linux
	// installer ISOs (Proxmox, Debian, Ubuntu...) are hybrid and boot when
	// written to USB as-is; Windows installer ISOs are not.
	Hybrid bool
}

var extensions = map[string]bool{".iso": true, ".img": true}

// Discover lists the .iso and .img files directly inside dirs, newest
// first. Missing folders and duplicates are ignored.
func Discover(dirs []string) []Candidate {
	seen := map[string]bool{}
	var out []Candidate
	for _, dir := range dirs {
		if dir == "" {
			continue
		}
		dir = filepath.Clean(dir)
		if seen[dir] {
			continue
		}
		seen[dir] = true
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if !extensions[strings.ToLower(filepath.Ext(e.Name()))] {
				continue
			}
			path := filepath.Join(dir, e.Name())
			st, err := os.Stat(path) // follows symlinks
			if err != nil || !st.Mode().IsRegular() || st.Size() == 0 {
				continue
			}
			out = append(out, Candidate{Path: path, Size: st.Size(), Modified: st.ModTime()})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Modified.After(out[j].Modified) })
	if len(out) > MaxCandidates {
		out = out[:MaxCandidates]
	}
	return out
}

// DefaultDirs are the folders scanned for images: the user's downloads and
// desktop folders (including localized XDG folders on Linux) and the
// current directory.
func DefaultDirs(home string) []string {
	var dirs []string
	if home != "" {
		dirs = append(dirs, xdgDirs(home)...)
		dirs = append(dirs, filepath.Join(home, "Downloads"), filepath.Join(home, "Desktop"))
	}
	if cwd, err := os.Getwd(); err == nil {
		dirs = append(dirs, cwd)
	}
	return dirs
}

// HomeDir returns the invoking user's home folder, also when the program
// was relaunched through sudo (where $HOME may point at root's home).
func HomeDir() string {
	if name := os.Getenv("SUDO_USER"); name != "" && name != "root" {
		if u, err := user.Lookup(name); err == nil && u.HomeDir != "" {
			return u.HomeDir
		}
		switch runtime.GOOS {
		case "darwin":
			return filepath.Join("/Users", name)
		case "linux":
			return filepath.Join("/home", name)
		}
	}
	home, _ := os.UserHomeDir()
	return home
}

// xdgDirs reads the download and desktop folders from
// ~/.config/user-dirs.dirs, which Linux desktops localize ("Descargas").
func xdgDirs(home string) []string {
	f, err := os.Open(filepath.Join(home, ".config", "user-dirs.dirs"))
	if err != nil {
		return nil
	}
	defer f.Close()
	found := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		key, value, ok := strings.Cut(strings.TrimSpace(scanner.Text()), "=")
		if !ok || strings.HasPrefix(key, "#") {
			continue
		}
		value = strings.ReplaceAll(strings.Trim(value, `"`), "$HOME", home)
		if value != "" && filepath.Clean(value) != filepath.Clean(home) {
			found[key] = filepath.Clean(value)
		}
	}
	var dirs []string
	for _, key := range []string{"XDG_DOWNLOAD_DIR", "XDG_DESKTOP_DIR"} {
		if dir, ok := found[key]; ok {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

// Inspect checks that path is a non-empty regular file and reads its boot
// signature.
func Inspect(path string) (Info, error) {
	st, err := os.Stat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Info{}, fmt.Errorf("%s does not exist", path)
	case err != nil:
		return Info{}, err
	case st.IsDir():
		return Info{}, fmt.Errorf("%s is a folder, not an image file", path)
	case !st.Mode().IsRegular():
		return Info{}, fmt.Errorf("%s is not a regular file", path)
	case st.Size() == 0:
		return Info{}, fmt.Errorf("%s is empty", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return Info{}, err
	}
	defer f.Close()
	header := make([]byte, 512)
	n, err := io.ReadFull(f, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return Info{}, fmt.Errorf("read %s: %w", path, err)
	}
	return Info{
		Path:   path,
		Size:   st.Size(),
		Hybrid: n == 512 && header[510] == 0x55 && header[511] == 0xAA,
	}, nil
}

// Expand turns what a user typed or pasted into a clean path: surrounding
// spaces and quotes are removed, shell-escaped spaces from a drag and drop
// are unescaped, and a leading ~ becomes home.
func Expand(path, home string) string {
	path = strings.TrimSpace(path)
	if len(path) >= 2 && (path[0] == '"' || path[0] == '\'') && path[len(path)-1] == path[0] {
		path = path[1 : len(path)-1]
	}
	if runtime.GOOS != "windows" {
		path = strings.ReplaceAll(path, `\ `, " ")
	}
	if path == "~" {
		path = home
	} else if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		path = filepath.Join(home, path[2:])
	}
	if path == "" {
		return ""
	}
	return filepath.Clean(path)
}

// Shorten replaces the home folder prefix with ~ for display.
func Shorten(path, home string) string {
	if home != "" {
		if rel, err := filepath.Rel(home, path); err == nil && !strings.HasPrefix(rel, "..") && rel != "." {
			return filepath.Join("~", rel)
		}
	}
	return path
}
