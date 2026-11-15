//go:build windows

package win

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	procSHGetFileInfoW = shell32.NewProc("SHGetFileInfoW")
	procGetIconInfo    = user32.NewProc("GetIconInfo")
	procGetDC          = user32.NewProc("GetDC")
	procReleaseDC      = user32.NewProc("ReleaseDC")
	procGetObjectW     = gdi32.NewProc("GetObjectW")
	procGetDIBits      = gdi32.NewProc("GetDIBits")
	procDeleteObject   = gdi32.NewProc("DeleteObject")
)

const (
	shgfiIcon      = 0x000000100
	shgfiLargeIcon = 0x000000000

	maxShortcutSize = 1 << 20
	maxIconSide     = 512
)

type Installed struct {
	Name     string
	Exe      string
	IconPath string
}

var (
	skipNames   = []string{"uninstall", "удал", "деинстал", "readme", "release notes", "documentation", "license", "website", "справка", "repair"}
	skipTargets = []string{"unins", "uninstall", "msiexec.exe", "rundll32.exe", "update.exe", "setup.exe"}
)

func InstalledPrograms() []Installed {
	var roots []string
	if dir, err := os.UserConfigDir(); err == nil {
		roots = append(roots, filepath.Join(dir, `Microsoft\Windows\Start Menu\Programs`))
	}
	if dir := os.Getenv("ProgramData"); dir != "" {
		roots = append(roots, filepath.Join(dir, `Microsoft\Windows\Start Menu\Programs`))
	}

	byExe := make(map[string]Installed)
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.EqualFold(filepath.Ext(path), ".lnk") {
				return nil
			}
			item, ok := readShortcut(path)
			if !ok {
				return nil
			}
			if prev, seen := byExe[item.Exe]; !seen || len(item.Name) < len(prev.Name) {
				byExe[item.Exe] = item
			}
			return nil
		})
	}

	out := make([]Installed, 0, len(byExe))
	for _, item := range byExe {
		out = append(out, item)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func readShortcut(path string) (Installed, bool) {
	info, err := os.Stat(path)
	if err != nil || info.Size() > maxShortcutSize {
		return Installed{}, false
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Installed{}, false
	}
	sc, err := ParseShortcut(data, decodeANSI)
	if err != nil {
		return Installed{}, false
	}
	program := sc.Program()
	if !strings.EqualFold(filepath.Ext(program), ".exe") {
		return Installed{}, false
	}

	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	exe := baseName(program)
	lower := strings.ToLower(name)
	for _, word := range skipNames {
		if strings.Contains(lower, word) {
			return Installed{}, false
		}
	}
	for _, prefix := range skipTargets {
		if strings.HasPrefix(exe, prefix) {
			return Installed{}, false
		}
	}
	icon := path
	for _, candidate := range []string{program, sc.Target} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			icon = candidate
			break
		}
	}
	return Installed{Name: name, Exe: exe, IconPath: icon}, true
}

func decodeANSI(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	buf := make([]uint16, len(b))
	const systemCodePage = 0 // CP_ACP
	n, err := windows.MultiByteToWideChar(systemCodePage, 0, &b[0], int32(len(b)), &buf[0], int32(len(buf)))
	if err != nil {
		return string(b)
	}
	return windows.UTF16ToString(buf[:n])
}

type shFileInfo struct {
	icon        uintptr
	iconIndex   int32
	attributes  uint32
	displayName [260]uint16
	typeName    [80]uint16
}

type iconInfo struct {
	isIcon   int32
	xHotspot uint32
	yHotspot uint32
	mask     uintptr
	color    uintptr
}

type gdiBitmap struct {
	kind       int32
	width      int32
	height     int32
	widthBytes int32
	planes     uint16
	bitsPixel  uint16
	bits       uintptr
}

type bitmapInfo struct {
	size          uint32
	width         int32
	height        int32
	planes        uint16
	bitCount      uint16
	compression   uint32
	sizeImage     uint32
	xPelsPerMeter int32
	yPelsPerMeter int32
	clrUsed       uint32
	clrImportant  uint32
	colors        [3]uint32
}

func IconPNG(path string) ([]byte, error) {
	ptr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("icon of %s: %w", path, err)
	}
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	const alreadyInitialized = syscall.Errno(1) // S_FALSE: COM в потоке уже включён
	if err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED); err == nil || errors.Is(err, alreadyInitialized) {
		defer windows.CoUninitialize()
	}

	var info shFileInfo
	ok, _, _ := procSHGetFileInfoW.Call(uintptr(unsafe.Pointer(ptr)), 0,
		uintptr(unsafe.Pointer(&info)), unsafe.Sizeof(info), shgfiIcon|shgfiLargeIcon)
	if ok == 0 || info.icon == 0 {
		return nil, fmt.Errorf("icon of %s: not available", path)
	}
	defer procDestroyIcon.Call(info.icon)

	img, err := iconImage(info.icon)
	if err != nil {
		return nil, fmt.Errorf("icon of %s: %w", path, err)
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return nil, fmt.Errorf("icon of %s: %w", path, err)
	}
	return buf.Bytes(), nil
}

func iconImage(icon uintptr) (*image.NRGBA, error) {
	var info iconInfo
	if ok, _, _ := procGetIconInfo.Call(icon, uintptr(unsafe.Pointer(&info))); ok == 0 {
		return nil, errors.New("icon info is not available")
	}
	for _, bitmap := range []uintptr{info.mask, info.color} {
		if bitmap != 0 {
			defer procDeleteObject.Call(bitmap)
		}
	}
	if info.color == 0 {
		return nil, errors.New("monochrome icons are not supported")
	}

	var bm gdiBitmap
	procGetObjectW.Call(info.color, unsafe.Sizeof(bm), uintptr(unsafe.Pointer(&bm)))
	w, h := int(bm.width), int(bm.height)
	if w <= 0 || h <= 0 || w > maxIconSide || h > maxIconSide {
		return nil, fmt.Errorf("unexpected icon size %dx%d", w, h)
	}

	pixels, err := bitmapPixels(info.color, w, h)
	if err != nil {
		return nil, err
	}
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	opaque := false
	for i := 0; i < w*h; i++ {
		b, g, r, a := pixels[4*i], pixels[4*i+1], pixels[4*i+2], pixels[4*i+3]
		img.SetNRGBA(i%w, i/w, color.NRGBA{R: r, G: g, B: b, A: a})
		opaque = opaque || a != 0
	}
	if opaque {
		return img, nil
	}
	mask, err := bitmapPixels(info.mask, w, h)
	if err != nil {
		return nil, err
	}
	for i := 0; i < w*h; i++ {
		c := img.NRGBAAt(i%w, i/w)
		c.A = 0xff
		if mask[4*i] != 0 {
			c.A = 0
		}
		img.SetNRGBA(i%w, i/w, c)
	}
	return img, nil
}

func bitmapPixels(bitmap uintptr, w, h int) ([]byte, error) {
	dc, _, _ := procGetDC.Call(0)
	if dc == 0 {
		return nil, errors.New("screen device context is not available")
	}
	defer procReleaseDC.Call(0, dc)

	info := bitmapInfo{width: int32(w), height: -int32(h), planes: 1, bitCount: 32}
	info.size = 40
	buf := make([]byte, 4*w*h)
	lines, _, _ := procGetDIBits.Call(dc, bitmap, 0, uintptr(h),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&info)), 0)
	if int(lines) != h {
		return nil, errors.New("bitmap bits are not available")
	}
	return buf, nil
}
