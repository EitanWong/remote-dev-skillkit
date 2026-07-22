package windowsentry

import (
	"strings"
	"testing"
)

func TestWindowsEntryCurlAcceptsOnlySystemExecutable(t *testing.T) {
	want := `C:\Windows\System32\curl.exe`
	for _, configured := range []string{"", want, `c:\windows\system32\CURL.EXE`} {
		got, err := windowsSystemCurlPath(`C:\Windows`, configured)
		if err != nil {
			t.Fatalf("system curl path %q rejected: %v", configured, err)
		}
		if !strings.EqualFold(got, want) {
			t.Fatalf("system curl path = %q, want %q", got, want)
		}
	}
	for _, configured := range []string{
		`curl.exe`,
		`C:\Tools\curl.exe`,
		`C:\Windows\SysWOW64\curl.exe`,
		`\\server\share\curl.exe`,
	} {
		if _, err := windowsSystemCurlPath(`C:\Windows`, configured); err == nil {
			t.Fatalf("non-system curl path %q was accepted", configured)
		}
	}
	for _, root := range []string{"", `Windows`, `\\server\Windows`, `C:\Windows\..\Tools`} {
		if _, err := windowsSystemCurlPath(root, ""); err == nil {
			t.Fatalf("unsafe SystemRoot %q was accepted", root)
		}
	}
}
