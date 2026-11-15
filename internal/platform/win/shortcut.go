package win

import (
	"encoding/binary"
	"errors"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

type Shortcut struct {
	Target string
	Arguments string
}

var ErrNotShortcut = errors.New("not a shell link")

const (
	lnkHeaderSize   = 0x4C
	lnkHasIDList    = 0x01
	lnkHasLinkInfo  = 0x02
	lnkHasName      = 0x04
	lnkHasRelPath   = 0x08
	lnkHasWorkDir   = 0x10
	lnkHasArguments = 0x20
	lnkIsUnicode    = 0x80

	lnkInfoLocalPath     = 0x01
	lnkInfoUnicodeHeader = 0x24
)

func ParseShortcut(data []byte, decodeANSI func([]byte) string) (Shortcut, error) {
	r := lnkReader{data: data}
	if len(data) < lnkHeaderSize || r.u32(0) != lnkHeaderSize {
		return Shortcut{}, ErrNotShortcut
	}
	flags := r.u32(20)
	pos := lnkHeaderSize

	if flags&lnkHasIDList != 0 {
		pos += 2 + int(r.u16(pos))
	}

	var out Shortcut
	if flags&lnkHasLinkInfo != 0 {
		size := int(r.u32(pos))
		if size < 28 || pos+size > len(data) {
			return Shortcut{}, ErrNotShortcut
		}
		info := lnkReader{data: data[pos : pos+size]}
		if info.u32(8)&lnkInfoLocalPath != 0 {
			if info.u32(4) >= lnkInfoUnicodeHeader {
				out.Target = info.wideZ(int(info.u32(28))) + info.wideZ(int(info.u32(32)))
			} else {
				out.Target = decodeANSI(info.bytesZ(int(info.u32(16)))) + decodeANSI(info.bytesZ(int(info.u32(24))))
			}
		}
		if info.bad {
			return Shortcut{}, ErrNotShortcut
		}
		pos += size
	}

	for _, flag := range []uint32{lnkHasName, lnkHasRelPath, lnkHasWorkDir, lnkHasArguments} {
		if flags&flag == 0 {
			continue
		}
		count := int(r.u16(pos))
		pos += 2
		var s string
		if flags&lnkIsUnicode != 0 {
			s = r.wide(pos, count)
			pos += 2 * count
		} else {
			s = decodeANSI(r.slice(pos, count))
			pos += count
		}
		if flag == lnkHasArguments {
			out.Arguments = s
		}
	}
	if r.bad {
		return Shortcut{}, ErrNotShortcut
	}
	return out, nil
}

func (s Shortcut) Program() string {
	if !strings.EqualFold(filepath.Base(s.Target), "update.exe") {
		return s.Target
	}
	fields := strings.Fields(s.Arguments)
	for i, f := range fields {
		name := ""
		switch {
		case strings.EqualFold(f, "--processStart") && i+1 < len(fields):
			name = fields[i+1]
		case strings.HasPrefix(strings.ToLower(f), "--processstart="):
			name = f[len("--processstart="):]
		}
		if name = strings.Trim(name, `"`); name != "" {
			return filepath.Join(filepath.Dir(s.Target), name)
		}
	}
	return s.Target
}

type lnkReader struct {
	data []byte
	bad  bool
}

func (r *lnkReader) slice(off, n int) []byte {
	if off < 0 || n < 0 || off+n > len(r.data) {
		r.bad = true
		return nil
	}
	return r.data[off : off+n]
}

func (r *lnkReader) u16(off int) uint16 {
	if b := r.slice(off, 2); b != nil {
		return binary.LittleEndian.Uint16(b)
	}
	return 0
}

func (r *lnkReader) u32(off int) uint32 {
	if b := r.slice(off, 4); b != nil {
		return binary.LittleEndian.Uint32(b)
	}
	return 0
}

func (r *lnkReader) bytesZ(off int) []byte {
	if off < 0 || off > len(r.data) {
		r.bad = true
		return nil
	}
	for end := off; end < len(r.data); end++ {
		if r.data[end] == 0 {
			return r.data[off:end]
		}
	}
	r.bad = true
	return nil
}

func (r *lnkReader) wide(off, count int) string {
	b := r.slice(off, 2*count)
	units := make([]uint16, 0, count)
	for i := 0; i+1 < len(b); i += 2 {
		units = append(units, binary.LittleEndian.Uint16(b[i:]))
	}
	return string(utf16.Decode(units))
}

func (r *lnkReader) wideZ(off int) string {
	if off < 0 || off > len(r.data) {
		r.bad = true
		return ""
	}
	var units []uint16
	for i := off; i+1 < len(r.data); i += 2 {
		u := binary.LittleEndian.Uint16(r.data[i:])
		if u == 0 {
			return string(utf16.Decode(units))
		}
		units = append(units, u)
	}
	r.bad = true
	return ""
}
