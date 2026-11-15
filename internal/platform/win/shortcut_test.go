package win

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
	"unicode/utf16"
)

func buildShortcut(target string, wideInfo bool, texts map[uint32]string, unicodeStrings bool) []byte {
	le := binary.LittleEndian
	var flags uint32 = lnkHasIDList | lnkHasLinkInfo
	if unicodeStrings {
		flags |= lnkIsUnicode
	}
	for flag := range texts {
		flags |= flag
	}

	header := make([]byte, lnkHeaderSize)
	le.PutUint32(header, lnkHeaderSize)
	le.PutUint32(header[20:], flags)

	out := bytes.NewBuffer(header)
	idList := []byte{0x05, 0x00, 1, 2, 3, 0x00, 0x00}
	_ = binary.Write(out, le, uint16(len(idList)))
	out.Write(idList)

	headerSize := 28
	if wideInfo {
		headerSize = 36
	}
	ansi := append([]byte(target), 0)
	body := append([]byte{}, ansi...)
	suffixOff := headerSize + len(ansi)
	body = append(body, 0)
	wideOff, wideSuffixOff := 0, 0
	if wideInfo {
		wideOff = headerSize + len(body)
		for _, u := range utf16.Encode([]rune(target)) {
			body = binary.LittleEndian.AppendUint16(body, u)
		}
		body = append(body, 0, 0)
		wideSuffixOff = headerSize + len(body)
		body = append(body, 0, 0)
	}
	info := make([]byte, headerSize)
	le.PutUint32(info, uint32(headerSize+len(body)))
	le.PutUint32(info[4:], uint32(headerSize))
	le.PutUint32(info[8:], lnkInfoLocalPath)
	le.PutUint32(info[16:], uint32(headerSize))
	le.PutUint32(info[24:], uint32(suffixOff))
	if wideInfo {
		le.PutUint32(info[28:], uint32(wideOff))
		le.PutUint32(info[32:], uint32(wideSuffixOff))
	}
	out.Write(info)
	out.Write(body)

	for _, flag := range []uint32{lnkHasName, lnkHasRelPath, lnkHasWorkDir, lnkHasArguments} {
		s, ok := texts[flag]
		if !ok {
			continue
		}
		if unicodeStrings {
			units := utf16.Encode([]rune(s))
			_ = binary.Write(out, le, uint16(len(units)))
			_ = binary.Write(out, le, units)
		} else {
			_ = binary.Write(out, le, uint16(len(s)))
			out.WriteString(s)
		}
	}
	return out.Bytes()
}

func latin(b []byte) string { return string(b) }

func TestParseShortcut(t *testing.T) {
	tests := []struct {
		name        string
		data        []byte
		wantTarget  string
		wantArgs    string
		wantProgram string
	}{
		{
			name:        "plain program",
			data:        buildShortcut(`C:\Program Files\Microsoft VS Code\Code.exe`, false, nil, false),
			wantTarget:  `C:\Program Files\Microsoft VS Code\Code.exe`,
			wantProgram: `C:\Program Files\Microsoft VS Code\Code.exe`,
		},
		{
			name:        "unicode path is preferred",
			data:        buildShortcut(`C:\Users\Аня\AppData\Local\Яндекс Музыка\Яндекс Музыка.exe`, true, nil, true),
			wantTarget:  `C:\Users\Аня\AppData\Local\Яндекс Музыка\Яндекс Музыка.exe`,
			wantProgram: `C:\Users\Аня\AppData\Local\Яндекс Музыка\Яндекс Музыка.exe`,
		},
		{
			name: "arguments after other strings",
			data: buildShortcut(`C:\Tools\app.exe`, false,
				map[uint32]string{lnkHasName: "My app", lnkHasWorkDir: `C:\Tools`, lnkHasArguments: "--profile work"}, true),
			wantTarget:  `C:\Tools\app.exe`,
			wantArgs:    "--profile work",
			wantProgram: `C:\Tools\app.exe`,
		},
		{
			name: "squirrel launcher points at the real program",
			data: buildShortcut(`C:\Users\a\AppData\Local\Discord\Update.exe`, false,
				map[uint32]string{lnkHasArguments: "--processStart Discord.exe"}, false),
			wantTarget:  `C:\Users\a\AppData\Local\Discord\Update.exe`,
			wantArgs:    "--processStart Discord.exe",
			wantProgram: `C:\Users\a\AppData\Local\Discord\Discord.exe`,
		},
		{
			name: "squirrel launcher with quoted name and extra arguments",
			data: buildShortcut(`C:\Users\a\AppData\Local\slack\Update.exe`, false,
				map[uint32]string{lnkHasArguments: `--processStart "slack.exe" --process-start-args "--startup"`}, true),
			wantTarget:  `C:\Users\a\AppData\Local\slack\Update.exe`,
			wantArgs:    `--processStart "slack.exe" --process-start-args "--startup"`,
			wantProgram: `C:\Users\a\AppData\Local\slack\slack.exe`,
		},
		{
			name: "update.exe without a program stays as is",
			data: buildShortcut(`C:\Apps\Update.exe`, false,
				map[uint32]string{lnkHasArguments: "--check"}, false),
			wantTarget:  `C:\Apps\Update.exe`,
			wantArgs:    "--check",
			wantProgram: `C:\Apps\Update.exe`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseShortcut(tt.data, latin)
			if err != nil {
				t.Fatalf("ParseShortcut: %v", err)
			}
			if got.Target != tt.wantTarget || got.Arguments != tt.wantArgs {
				t.Errorf("shortcut = %+v, want target %q and arguments %q", got, tt.wantTarget, tt.wantArgs)
			}
			if p := got.Program(); p != tt.wantProgram {
				t.Errorf("Program() = %q, want %q", p, tt.wantProgram)
			}
		})
	}
}

func TestParseShortcutRejectsDamagedFiles(t *testing.T) {
	good := buildShortcut(`C:\Tools\app.exe`, true, map[uint32]string{lnkHasArguments: "--x"}, true)

	tests := map[string][]byte{
		"empty":            nil,
		"not a shortcut":   []byte("this is a text file, not a shell link at all ........................................"),
		"header only":      good[:lnkHeaderSize],
		"cut in link info": good[:lnkHeaderSize+20],
		"cut in strings":   good[:len(good)-3],
	}
	for name, data := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseShortcut(data, latin); !errors.Is(err, ErrNotShortcut) {
				t.Errorf("ParseShortcut = %v, want %v", err, ErrNotShortcut)
			}
		})
	}

	for n := range good {
		if _, err := ParseShortcut(good[:n], latin); err == nil {
			t.Fatalf("truncated to %d of %d bytes: parsed without an error", n, len(good))
		}
	}
}
