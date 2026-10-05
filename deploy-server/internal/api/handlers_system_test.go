package api

import (
	"encoding/base64"
	"encoding/binary"
	"os"
	"strings"
	"testing"
	"unicode/utf16"
)

func TestProbeToolOutput(t *testing.T) {
	encodeUTF16 := func(s string, order binary.ByteOrder, bom bool) []byte {
		units := utf16.Encode([]rune(s))
		if bom {
			units = append([]uint16{0xfeff}, units...)
		}
		out := make([]byte, len(units)*2)
		for i, u := range units {
			order.PutUint16(out[2*i:], u)
		}
		return out
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name   string
		out    []byte
		stderr bool
		fail   bool
		want   string
	}{
		{name: "UTF16LE_BOM", out: encodeUTF16(" \r\n版本 😀 1.2\r\nignored", binary.LittleEndian, true), want: "版本 😀 1.2"},
		{name: "UTF16BE_BOM", out: encodeUTF16("版本 😀 1.2\r\nignored", binary.BigEndian, true), want: "版本 😀 1.2"},
		{name: "UTF16LE_NoBOM", out: []byte("A\x00c\x00c\x00e\x00s\x00s\x00 \x00i\x00s\x00 \x00d\x00e\x00n\x00i\x00e\x00d\x00.\x00"), want: "Access is denied."},
		{name: "UTF16BE_NoBOM", out: encodeUTF16("tool 1.2\r\nignored", binary.BigEndian, false), want: "tool 1.2"},
		{name: "UTF8", out: []byte(" \r\n工具 1.2 \r\nignored"), want: "工具 1.2"},
		{name: "InvalidUTF8", out: []byte("tool\xff\xfe 1.2\xc0\xaf\nignored"), want: "tool 1.2"},
		{name: "Empty", out: nil, want: ""},
		{name: "StderrSuccess", out: []byte("java version 1.2\n"), stderr: true, want: "java version 1.2"},
		{name: "FailedStderr", out: []byte("0 [main] bash (24944) fatal error - NtCreateDirectoryObject: 0xC0000022\n"), stderr: true, fail: true, want: ""},
		{name: "FailedStdout", out: []byte("tool 1.2\n"), fail: true, want: ""},
		{name: "FailedUTF16", out: encodeUTF16("Access is denied.", binary.LittleEndian, true), fail: true, want: ""},
		{name: "Limit200Characters", out: []byte(strings.Repeat("版😀", 101)), want: strings.Repeat("版😀", 100)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("DOCTOR_PROBE_HELPER", "1")
			t.Setenv("DOCTOR_PROBE_OUTPUT", base64.StdEncoding.EncodeToString(tc.out))
			t.Setenv("DOCTOR_PROBE_STDERR", "0")
			t.Setenv("DOCTOR_PROBE_FAIL", "0")
			if tc.stderr {
				t.Setenv("DOCTOR_PROBE_STDERR", "1")
			}
			if tc.fail {
				t.Setenv("DOCTOR_PROBE_FAIL", "1")
			}
			p := toolProbe{Name: executable, Args: []string{"-test.run=^TestProbeToolHelperProcess$"}, Hint: "test hint"}
			got := probeTool(p, os.Environ())
			if !got.Found || got.Path != executable || got.Name != p.Name || got.Hint != p.Hint {
				t.Fatalf("probe metadata changed: %+v", got)
			}
			if got.Version != tc.want {
				t.Errorf("Version = %q, want %q", got.Version, tc.want)
			}
		})
	}
}

// Run the test executable as a tool, without requiring a shell or installed tools.
func TestProbeToolHelperProcess(t *testing.T) {
	if os.Getenv("DOCTOR_PROBE_HELPER") != "1" {
		return
	}
	out, err := base64.StdEncoding.DecodeString(os.Getenv("DOCTOR_PROBE_OUTPUT"))
	if err != nil {
		os.Exit(2)
	}
	stream := os.Stdout
	if os.Getenv("DOCTOR_PROBE_STDERR") == "1" {
		stream = os.Stderr
	}
	if _, err := stream.Write(out); err != nil {
		os.Exit(2)
	}
	if os.Getenv("DOCTOR_PROBE_FAIL") == "1" {
		os.Exit(1)
	}
	os.Exit(0)
}

func TestFirstLine(t *testing.T) {
	for _, tc := range []struct {
		name string
		in   string
		want string
	}{
		{"WhitespaceAndCRLF", " \r\n  tool 1.2 \r\nignored", "tool 1.2"},
		{"ASCII200", strings.Repeat("a", 201), strings.Repeat("a", 200)},
		{"Unicode200", strings.Repeat("版😀", 101), strings.Repeat("版😀", 100)},
		{"TrimAfterLimit", strings.Repeat("a", 199) + " b", strings.Repeat("a", 199)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := firstLine(tc.in); got != tc.want {
				t.Errorf("firstLine() = %q, want %q", got, tc.want)
			}
		})
	}
}
