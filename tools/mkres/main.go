package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"dofirst/internal/logo"
)

// Типы ресурсов Windows
const (
	rtIcon      = 3
	rtGroupIcon = 14
	rtVersion   = 16
	rtManifest  = 24

	langNeutral = 0x0000
	langEnglish = 0x0409
	langRussian = 0x0419
	codepageUTF = 0x04B0 // 1200, UTF-16
)

var machines = map[string]struct {
	machine uint16
	reloc   uint16
}{
	"amd64": {0x8664, 0x0003}, // IMAGE_REL_AMD64_ADDR32NB
	"arm64": {0xAA64, 0x0002}, // IMAGE_REL_ARM64_ADDR32NB
}

const manifest = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="DoFirstTo" version="%s" processorArchitecture="*"/>
  <description>DoFirstTo</description>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true/pm</dpiAware>
      <dpiAwareness xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">PerMonitorV2</dpiAwareness>
      <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
    </windowsSettings>
  </application>
</assembly>
`

func main() {
	version := flag.String("version", "0.1.0", "версия приложения")
	out := flag.String("out", "cmd/dofirst", "каталог пакета main, куда положить .syso")
	flag.Parse()

	if err := run(*version, *out); err != nil {
		fmt.Fprintln(os.Stderr, "mkres:", err)
		os.Exit(1)
	}
}

func run(version, dir string) error {
	numbers, err := versionNumbers(version)
	if err != nil {
		return err
	}
	resources, err := collect(version, numbers)
	if err != nil {
		return err
	}
	for arch := range machines {
		path := filepath.Join(dir, "rsrc_windows_"+arch+".syso")
		if err := os.WriteFile(path, object(resources, arch), 0o644); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
		fmt.Println(path)
	}
	return nil
}

type resource struct {
	kind, id, lang uint16
	data           []byte
}

func collect(version string, numbers [4]uint16) ([]resource, error) {
	var out []resource

	sizes := []int{16, 20, 24, 32, 40, 48, 64, 256}
	group := new(bytes.Buffer)
	put(group, uint16(0), uint16(1), uint16(len(sizes)))
	for i, size := range sizes {
		var data []byte
		if size >= 256 {
			png, err := logo.PNG(size, logo.App)
			if err != nil {
				return nil, err
			}
			data = png
		} else {
			data = dib(logo.Render(size, logo.App))
		}
		id := uint16(i + 1)
		out = append(out, resource{rtIcon, id, langNeutral, data})
		put(group, uint8(size), uint8(size), uint8(0), uint8(0), uint16(1), uint16(32), uint32(len(data)), id)
	}
	out = append(out, resource{rtGroupIcon, 1, langNeutral, group.Bytes()})

	out = append(out, resource{rtVersion, 1, langRussian, versionInfo(version, numbers)})

	four := fmt.Sprintf("%d.%d.%d.%d", numbers[0], numbers[1], numbers[2], numbers[3])
	out = append(out, resource{rtManifest, 1, langEnglish, []byte(fmt.Sprintf(manifest, four))})
	return out, nil
}

func versionNumbers(v string) ([4]uint16, error) {
	var out [4]uint16
	core, _, _ := strings.Cut(strings.TrimPrefix(v, "v"), "-")
	parts := strings.Split(core, ".")
	if core == "" || len(parts) > 4 {
		return out, fmt.Errorf("version %q: want up to four numbers", v)
	}
	for i, p := range parts {
		n, err := strconv.ParseUint(p, 10, 16)
		if err != nil {
			return out, fmt.Errorf("version %q: %w", v, err)
		}
		out[i] = uint16(n)
	}
	return out, nil
}

func dib(img *image.NRGBA) []byte {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	buf := new(bytes.Buffer)
	// Высота удвоена: по формату в ней учитывается и маска.
	put(buf, uint32(40), int32(w), int32(2*h), uint16(1), uint16(32), uint32(0), uint32(0), int32(0), int32(0), uint32(0), uint32(0))
	for y := h - 1; y >= 0; y-- {
		for x := range w {
			c := img.NRGBAAt(x, y)
			buf.Write([]byte{c.B, c.G, c.R, c.A})
		}
	}
	maskRow := (w + 31) / 32 * 4
	buf.Write(make([]byte, maskRow*h))
	return buf.Bytes()
}

// ---------- сведения о версии ---------------------------------------------------

func utf16z(s string) []byte {
	buf := new(bytes.Buffer)
	put(buf, utf16.Encode([]rune(s)), uint16(0))
	return buf.Bytes()
}

func pad4(b []byte) []byte {
	for len(b)%4 != 0 {
		b = append(b, 0)
	}
	return b
}

func node(key string, text bool, value []byte, valueLen int, children ...[]byte) []byte {
	kind := uint16(0)
	if text {
		kind = 1
	}
	body := pad4(append(make([]byte, 6), utf16z(key)...))
	body = append(body, value...)
	for _, child := range children {
		body = append(pad4(body), child...)
	}
	binary.LittleEndian.PutUint16(body[0:], uint16(len(body)))
	binary.LittleEndian.PutUint16(body[2:], uint16(valueLen))
	binary.LittleEndian.PutUint16(body[4:], kind)
	return body
}

func versionInfo(version string, n [4]uint16) []byte {
	fixed := new(bytes.Buffer)
	ms, ls := uint32(n[0])<<16|uint32(n[1]), uint32(n[2])<<16|uint32(n[3])
	put(fixed,
		uint32(0xFEEF04BD), uint32(0x00010000), // подпись и версия структуры
		ms, ls, ms, ls, // версия файла и продукта
		uint32(0x3F), uint32(0), // маска и флаги
		uint32(0x00040004), // VOS_NT_WINDOWS32
		uint32(1),          // VFT_APP
		uint32(0), uint32(0), uint32(0))

	entries := [][2]string{
		{"CompanyName", "DoFirstTo"},
		{"FileDescription", "DoFirstTo — сначала дело"},
		{"FileVersion", version},
		{"InternalName", "dofirst"},
		{"OriginalFilename", "dofirst.exe"},
		{"ProductName", "DoFirstTo"},
		{"ProductVersion", version},
	}
	var items [][]byte
	for _, e := range entries {
		value := utf16z(e[1])
		items = append(items, node(e[0], true, value, len(value)/2))
	}
	table := node(fmt.Sprintf("%04X%04X", langRussian, codepageUTF), true, nil, 0, items...)
	stringInfo := node("StringFileInfo", true, nil, 0, table)

	translation := new(bytes.Buffer)
	put(translation, uint16(langRussian), uint16(codepageUTF))
	varInfo := node("VarFileInfo", true, nil, 0, node("Translation", false, translation.Bytes(), translation.Len()))

	return node("VS_VERSION_INFO", false, fixed.Bytes(), fixed.Len(), stringInfo, varInfo)
}

// ---------- объектный файл ---------------------------------------------------------

func object(resources []resource, arch string) []byte {
	sort.Slice(resources, func(i, j int) bool {
		a, b := resources[i], resources[j]
		if a.kind != b.kind {
			return a.kind < b.kind
		}
		return a.id < b.id
	})
	var kinds []uint16
	byKind := map[uint16][]resource{}
	for _, r := range resources {
		if _, seen := byKind[r.kind]; !seen {
			kinds = append(kinds, r.kind)
		}
		byKind[r.kind] = append(byKind[r.kind], r)
	}

	const dirSize, entrySize, dataEntrySize = 16, 8, 16
	const subdirectory = 0x80000000

	kindDirAt := make(map[uint16]int)
	offset := dirSize + entrySize*len(kinds)
	for _, k := range kinds {
		kindDirAt[k] = offset
		offset += dirSize + entrySize*len(byKind[k])
	}
	langDirAt := offset
	dataEntryAt := langDirAt + (dirSize+entrySize)*len(resources)
	blobAt := dataEntryAt + dataEntrySize*len(resources)

	section := make([]byte, blobAt)
	le := binary.LittleEndian
	dir := func(at, entries int) {
		le.PutUint16(section[at+14:], uint16(entries))
	}
	entry := func(at int, id uint16, target uint32) {
		le.PutUint32(section[at:], uint32(id))
		le.PutUint32(section[at+4:], target)
	}

	var relocs []uint32
	dir(0, len(kinds))
	index := 0
	for ki, k := range kinds {
		entry(dirSize+entrySize*ki, k, subdirectory|uint32(kindDirAt[k]))
		dir(kindDirAt[k], len(byKind[k]))
		for ri, r := range byKind[k] {
			langDir := langDirAt + (dirSize+entrySize)*index
			dataEntry := dataEntryAt + dataEntrySize*index
			entry(kindDirAt[k]+dirSize+entrySize*ri, r.id, subdirectory|uint32(langDir))
			dir(langDir, 1)
			entry(langDir+dirSize, r.lang, uint32(dataEntry))
			le.PutUint32(section[dataEntry:], uint32(len(section)))
			le.PutUint32(section[dataEntry+4:], uint32(len(r.data)))
			relocs = append(relocs, uint32(dataEntry))

			section = append(section, r.data...)
			for len(section)%8 != 0 {
				section = append(section, 0)
			}
			index++
		}
	}

	const headerSize, sectionHeaderSize = 20, 40
	const readableData = 0x40000040 // IMAGE_SCN_CNT_INITIALIZED_DATA / IMAGE_SCN_MEM_READ
	m := machines[arch]
	dataAt := headerSize + sectionHeaderSize
	relocAt := dataAt + len(section)
	symbolsAt := relocAt + 10*len(relocs)
	name := [8]byte{'.', 'r', 's', 'r', 'c'}

	buf := new(bytes.Buffer)
	// Заголовок файла
	put(buf, m.machine, uint16(1), uint32(0), uint32(symbolsAt), uint32(1), uint16(0), uint16(0))
	// Заголовок секции
	put(buf, name, uint32(0), uint32(0), uint32(len(section)), uint32(dataAt), uint32(relocAt), uint32(0),
		uint16(len(relocs)), uint16(0), uint32(readableData))
	buf.Write(section)
	for _, at := range relocs {
		put(buf, at, uint32(0), m.reloc)
	}
	put(buf, name, uint32(0), int16(1), uint16(0), uint8(3), uint8(0))
	put(buf, uint32(4))
	return buf.Bytes()
}

func put(buf *bytes.Buffer, values ...any) {
	for _, v := range values {
		if err := binary.Write(buf, binary.LittleEndian, v); err != nil {
			panic(err)
		}
	}
}
