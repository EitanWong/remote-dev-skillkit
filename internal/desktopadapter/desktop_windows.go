//go:build windows

package desktopadapter

import (
	"context"
	"encoding/base64"
	"fmt"
	"image"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

var (
	user32                  = syscall.NewLazyDLL("user32.dll")
	gdi32                   = syscall.NewLazyDLL("gdi32.dll")
	shell32                 = syscall.NewLazyDLL("shell32.dll")
	kernel32                = syscall.NewLazyDLL("kernel32.dll")
	procEnumWindows         = user32.NewProc("EnumWindows")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procGetWindowTextLength = user32.NewProc("GetWindowTextLengthW")
	procGetWindowText       = user32.NewProc("GetWindowTextW")
	procGetWindowThreadPID  = user32.NewProc("GetWindowThreadProcessId")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procSetWindowPos        = user32.NewProc("SetWindowPos")
	procPostMessage         = user32.NewProc("PostMessageW")
	procSetCursorPos        = user32.NewProc("SetCursorPos")
	procSendInput           = user32.NewProc("SendInput")
	procGetDC               = user32.NewProc("GetDC")
	procReleaseDC           = user32.NewProc("ReleaseDC")
	procGetSystemMetrics    = user32.NewProc("GetSystemMetrics")
	procCreateCompatibleDC  = gdi32.NewProc("CreateCompatibleDC")
	procDeleteDC            = gdi32.NewProc("DeleteDC")
	procCreateCompatibleBMP = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject        = gdi32.NewProc("SelectObject")
	procDeleteObject        = gdi32.NewProc("DeleteObject")
	procBitBlt              = gdi32.NewProc("BitBlt")
	procGetDIBits           = gdi32.NewProc("GetDIBits")
	procShellExecute        = shell32.NewProc("ShellExecuteW")
	procOpenClipboard       = user32.NewProc("OpenClipboard")
	procCloseClipboard      = user32.NewProc("CloseClipboard")
	procEmptyClipboard      = user32.NewProc("EmptyClipboard")
	procGetClipboardData    = user32.NewProc("GetClipboardData")
	procSetClipboardData    = user32.NewProc("SetClipboardData")
	procIsClipboardFormat   = user32.NewProc("IsClipboardFormatAvailable")
	procGlobalAlloc         = kernel32.NewProc("GlobalAlloc")
	procGlobalFree          = kernel32.NewProc("GlobalFree")
	procGlobalLock          = kernel32.NewProc("GlobalLock")
	procGlobalUnlock        = kernel32.NewProc("GlobalUnlock")
	procGlobalSize          = kernel32.NewProc("GlobalSize")
)

const (
	wmClose            = 0x0010
	swShownormal       = 1
	swpNoZOrder        = 0x0004
	swpShowWindow      = 0x0040
	inputKeyboard      = 1
	inputMouse         = 0
	keyeventfKeyUp     = 0x0002
	keyeventfUnicode   = 0x0004
	mouseeventfMove    = 0x0001
	mouseeventfLeftUp  = 0x0004
	mouseeventfLeftDn  = 0x0002
	mouseeventfRightUp = 0x0010
	mouseeventfRightDn = 0x0008
	smCXScreen         = 0
	smCYScreen         = 1
	srccopy            = 0x00CC0020
	biRGB              = 0
	dibRGBColors       = 0
	cfUnicodeText      = 13
	gmemMoveable       = 0x0002
	gmemZeroinit       = 0x0040
)

type keyboardInput struct {
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type mouseInput struct {
	Dx          int32
	Dy          int32
	MouseData   uint32
	DwFlags     uint32
	Time        uint32
	DwExtraInfo uintptr
}

type input struct {
	Type uint32
	_    uint32
	Data [32]byte
}

type bitmapInfoHeader struct {
	Size          uint32
	Width         int32
	Height        int32
	Planes        uint16
	BitCount      uint16
	Compression   uint32
	SizeImage     uint32
	XPelsPerMeter int32
	YPelsPerMeter int32
	ClrUsed       uint32
	ClrImportant  uint32
}

type bitmapInfo struct {
	Header bitmapInfoHeader
	Colors [3]uint32
}

func executePlatform(ctx context.Context, spec Spec) (ResultArtifact, error) {
	switch spec.Action {
	case "window.inspect":
		windows, err := enumWindows()
		return ResultArtifact{Windows: windows, Status: "succeeded"}, err
	case "window.focus":
		hwnd, err := resolveWindow(spec)
		if err != nil {
			return ResultArtifact{Status: "failed", DesktopSessionState: "desktop_session_unavailable"}, err
		}
		ret, _, callErr := procSetForegroundWindow.Call(hwnd)
		if ret == 0 {
			return ResultArtifact{Status: "failed", WindowID: formatHWND(hwnd)}, fmt.Errorf("SetForegroundWindow: %w", callErr)
		}
		return ResultArtifact{Status: "succeeded", WindowID: formatHWND(hwnd)}, nil
	case "window.move":
		hwnd, err := resolveWindow(spec)
		if err != nil {
			return ResultArtifact{Status: "failed", DesktopSessionState: "desktop_session_unavailable"}, err
		}
		width, height := spec.Width, spec.Height
		if width <= 0 {
			width = 800
		}
		if height <= 0 {
			height = 600
		}
		ret, _, callErr := procSetWindowPos.Call(hwnd, 0, uintptr(spec.X), uintptr(spec.Y), uintptr(width), uintptr(height), swpNoZOrder|swpShowWindow)
		if ret == 0 {
			return ResultArtifact{Status: "failed", WindowID: formatHWND(hwnd)}, fmt.Errorf("SetWindowPos: %w", callErr)
		}
		return ResultArtifact{Status: "succeeded", WindowID: formatHWND(hwnd)}, nil
	case "screen.screenshot":
		captureBudget := pngBudget(spec.MaxOutputBytes, 1)
		if strings.TrimSpace(spec.OutputPath) != "" {
			captureBudget = 16 * 1024 * 1024
		}
		pngBytes, err := captureScreenPNG(captureBudget)
		if err != nil {
			return ResultArtifact{Status: "failed", DesktopSessionState: "desktop_session_unavailable"}, err
		}
		if strings.TrimSpace(spec.OutputPath) != "" {
			persisted, err := persistDesktopArtifact(spec.WorkspaceRoot, spec.OutputPath, "image/png", pngBytes)
			if err != nil {
				return ResultArtifact{Status: "failed"}, err
			}
			return ResultArtifact{Status: "succeeded", ArtifactPath: persisted.Path, ArtifactContentType: persisted.ContentType, ArtifactBytes: persisted.Bytes, ArtifactSHA256: persisted.SHA256}, nil
		}
		return ResultArtifact{Status: "succeeded", PNGBase64: base64.StdEncoding.EncodeToString(pngBytes)}, nil
	case "screen.record":
		frames := spec.Frames
		if frames <= 0 {
			frames = 3
		}
		if frames > 30 {
			frames = 30
		}
		interval := time.Duration(spec.IntervalMillis) * time.Millisecond
		if interval <= 0 {
			interval = 500 * time.Millisecond
		}
		result := ResultArtifact{Status: "succeeded"}
		capturedFrames := make([][]byte, 0, frames)
		captureBudget := 16 * 1024 * 1024
		if strings.TrimSpace(spec.OutputPath) == "" {
			captureBudget = pngBudget(spec.MaxOutputBytes, frames)
		}
		for i := 0; i < frames; i++ {
			select {
			case <-ctx.Done():
				result.Status = "canceled"
				return result, ctx.Err()
			default:
			}
			pngBytes, err := captureScreenPNG(captureBudget)
			if err != nil {
				result.Status = "failed"
				result.DesktopSessionState = "desktop_session_unavailable"
				return result, err
			}
			capturedFrames = append(capturedFrames, pngBytes)
			result.Frames = append(result.Frames, encodeFrame(i, pngBytes))
			if i+1 < frames {
				time.Sleep(interval)
			}
		}
		if strings.TrimSpace(spec.OutputPath) != "" {
			bundle, err := encodeFrameBundle(capturedFrames)
			if err != nil {
				return ResultArtifact{Status: "failed"}, err
			}
			persisted, err := persistDesktopArtifact(spec.WorkspaceRoot, spec.OutputPath, "application/zip", bundle)
			if err != nil {
				return ResultArtifact{Status: "failed"}, err
			}
			return ResultArtifact{Status: "succeeded", ArtifactPath: persisted.Path, ArtifactContentType: persisted.ContentType, ArtifactBytes: persisted.Bytes, ArtifactSHA256: persisted.SHA256}, nil
		}
		return result, nil
	case "input.keyboard":
		if strings.TrimSpace(spec.Text) == "" {
			return ResultArtifact{Status: "failed"}, fmt.Errorf("text is required for keyboard input")
		}
		if err := sendUnicodeText(spec.Text); err != nil {
			return ResultArtifact{Status: "failed", DesktopSessionState: "desktop_session_unavailable"}, err
		}
		return ResultArtifact{Status: "succeeded", Detail: "keyboard text sent"}, nil
	case "input.mouse":
		if err := mouseAction(spec); err != nil {
			return ResultArtifact{Status: "failed", DesktopSessionState: "desktop_session_unavailable"}, err
		}
		return ResultArtifact{Status: "succeeded", Detail: "mouse action sent"}, nil
	case "app.launch":
		target := strings.TrimSpace(spec.App)
		if target == "" {
			return ResultArtifact{Status: "failed"}, fmt.Errorf("app is required")
		}
		if err := shellExecute(target); err != nil {
			return ResultArtifact{Status: "failed"}, err
		}
		return ResultArtifact{Status: "succeeded", Target: target}, nil
	case "url.open":
		target := strings.TrimSpace(spec.URL)
		if target == "" {
			return ResultArtifact{Status: "failed"}, fmt.Errorf("url is required")
		}
		if err := shellExecute(target); err != nil {
			return ResultArtifact{Status: "failed"}, err
		}
		return ResultArtifact{Status: "succeeded", Target: target}, nil
	case "clipboard.read":
		text, err := readClipboardText()
		if err != nil {
			return ResultArtifact{Status: "failed", DesktopSessionState: "desktop_session_unavailable"}, err
		}
		return ResultArtifact{Status: "succeeded", ClipboardText: text}, nil
	case "clipboard.write":
		if err := writeClipboardText(spec.Text); err != nil {
			return ResultArtifact{Status: "failed", DesktopSessionState: "desktop_session_unavailable"}, err
		}
		return ResultArtifact{Status: "succeeded", Detail: "clipboard text written"}, nil
	case "app.close":
		hwnd, err := resolveWindow(spec)
		if err != nil {
			return ResultArtifact{Status: "failed", DesktopSessionState: "desktop_session_unavailable"}, err
		}
		ret, _, callErr := procPostMessage.Call(hwnd, wmClose, 0, 0)
		if ret == 0 {
			return ResultArtifact{Status: "failed", WindowID: formatHWND(hwnd)}, fmt.Errorf("WM_CLOSE: %w", callErr)
		}
		return ResultArtifact{Status: "succeeded", WindowID: formatHWND(hwnd)}, nil
	default:
		return ResultArtifact{Status: "failed"}, fmt.Errorf("unsupported desktop action %q", spec.Action)
	}
}

func enumWindows() ([]Window, error) {
	var windows []Window
	cb := syscall.NewCallback(func(hwnd uintptr, lparam uintptr) uintptr {
		visible, _, _ := procIsWindowVisible.Call(hwnd)
		if visible == 0 {
			return 1
		}
		title := windowText(hwnd)
		if strings.TrimSpace(title) == "" {
			return 1
		}
		var pid uint32
		procGetWindowThreadPID.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
		windows = append(windows, Window{
			ID:        formatHWND(hwnd),
			Title:     title,
			ProcessID: pid,
			Visible:   true,
		})
		return 1
	})
	ret, _, err := procEnumWindows.Call(cb, 0)
	if ret == 0 {
		return windows, fmt.Errorf("EnumWindows: %w", err)
	}
	return windows, nil
}

func windowText(hwnd uintptr) string {
	length, _, _ := procGetWindowTextLength.Call(hwnd)
	if length == 0 {
		return ""
	}
	buf := make([]uint16, int(length)+1)
	procGetWindowText.Call(hwnd, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	return syscall.UTF16ToString(buf)
}

func resolveWindow(spec Spec) (uintptr, error) {
	if spec.WindowID != "" {
		return parseHWND(spec.WindowID)
	}
	title := strings.ToLower(strings.TrimSpace(spec.Title))
	if title == "" && strings.TrimSpace(spec.App) != "" {
		title = strings.ToLower(strings.TrimSpace(spec.App))
	}
	if title == "" {
		return 0, fmt.Errorf("window_id or title is required")
	}
	windows, err := enumWindows()
	if err != nil {
		return 0, err
	}
	for _, win := range windows {
		if strings.Contains(strings.ToLower(win.Title), title) {
			return parseHWND(win.ID)
		}
	}
	return 0, fmt.Errorf("window not found for title %q", title)
}

func parseHWND(value string) (uintptr, error) {
	value = strings.TrimSpace(value)
	value = strings.TrimPrefix(strings.ToLower(value), "0x")
	parsed, err := strconv.ParseUint(value, 16, 64)
	if err != nil {
		return 0, fmt.Errorf("parse window id: %w", err)
	}
	return uintptr(parsed), nil
}

func formatHWND(hwnd uintptr) string {
	return fmt.Sprintf("0x%X", hwnd)
}

func shellExecute(target string) error {
	verb, _ := syscall.UTF16PtrFromString("open")
	file, _ := syscall.UTF16PtrFromString(target)
	ret, _, err := procShellExecute.Call(0, uintptr(unsafe.Pointer(verb)), uintptr(unsafe.Pointer(file)), 0, 0, swShownormal)
	if ret <= 32 {
		return fmt.Errorf("ShellExecuteW failed: %w", err)
	}
	return nil
}

func sendUnicodeText(text string) error {
	var inputs []input
	for _, r := range text {
		down := keyboardInput{WScan: uint16(r), DwFlags: keyeventfUnicode}
		up := keyboardInput{WScan: uint16(r), DwFlags: keyeventfUnicode | keyeventfKeyUp}
		inputs = append(inputs, keyboardAsInput(down), keyboardAsInput(up))
	}
	if len(inputs) == 0 {
		return nil
	}
	ret, _, err := procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(input{}))
	if ret != uintptr(len(inputs)) {
		return fmt.Errorf("SendInput keyboard: %w", err)
	}
	return nil
}

func keyboardAsInput(value keyboardInput) input {
	var in input
	in.Type = inputKeyboard
	*(*keyboardInput)(unsafe.Pointer(&in.Data[0])) = value
	return in
}

func mouseAction(spec Spec) error {
	ret, _, err := procSetCursorPos.Call(uintptr(spec.X), uintptr(spec.Y))
	if ret == 0 {
		return fmt.Errorf("SetCursorPos: %w", err)
	}
	button := strings.ToLower(strings.TrimSpace(spec.Button))
	if button == "" || button == "move" {
		return nil
	}
	var down, up uint32
	switch button {
	case "left", "click":
		down, up = mouseeventfLeftDn, mouseeventfLeftUp
	case "right":
		down, up = mouseeventfRightDn, mouseeventfRightUp
	default:
		return fmt.Errorf("unsupported mouse button %q", button)
	}
	inputs := []input{mouseAsInput(mouseInput{DwFlags: down}), mouseAsInput(mouseInput{DwFlags: up})}
	ret, _, err = procSendInput.Call(uintptr(len(inputs)), uintptr(unsafe.Pointer(&inputs[0])), unsafe.Sizeof(input{}))
	if ret != uintptr(len(inputs)) {
		return fmt.Errorf("SendInput mouse: %w", err)
	}
	return nil
}

func mouseAsInput(value mouseInput) input {
	var in input
	in.Type = inputMouse
	*(*mouseInput)(unsafe.Pointer(&in.Data[0])) = value
	return in
}

func readClipboardText() (string, error) {
	available, _, _ := procIsClipboardFormat.Call(cfUnicodeText)
	if available == 0 {
		return "", fmt.Errorf("clipboard unicode text unavailable")
	}
	if err := openClipboard(); err != nil {
		return "", err
	}
	defer procCloseClipboard.Call()
	handle, _, err := procGetClipboardData.Call(cfUnicodeText)
	if handle == 0 {
		return "", fmt.Errorf("GetClipboardData: %w", err)
	}
	ptr, _, err := procGlobalLock.Call(handle)
	if ptr == 0 {
		return "", fmt.Errorf("GlobalLock clipboard: %w", err)
	}
	defer procGlobalUnlock.Call(handle)
	size, _, _ := procGlobalSize.Call(handle)
	if size == 0 {
		return "", nil
	}
	chars := unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), int(size)/2)
	return syscall.UTF16ToString(chars), nil
}

func writeClipboardText(text string) error {
	utf16Text, err := syscall.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("encode clipboard text: %w", err)
	}
	byteSize := uintptr(len(utf16Text) * 2)
	handle, _, err := procGlobalAlloc.Call(gmemMoveable|gmemZeroinit, byteSize)
	if handle == 0 {
		return fmt.Errorf("GlobalAlloc clipboard: %w", err)
	}
	clipboardOwnsHandle := false
	defer func() {
		if !clipboardOwnsHandle {
			procGlobalFree.Call(handle)
		}
	}()
	ptr, _, err := procGlobalLock.Call(handle)
	if ptr == 0 {
		return fmt.Errorf("GlobalLock clipboard: %w", err)
	}
	copy(unsafe.Slice((*uint16)(unsafe.Pointer(ptr)), len(utf16Text)), utf16Text)
	procGlobalUnlock.Call(handle)
	if err := openClipboard(); err != nil {
		return err
	}
	defer procCloseClipboard.Call()
	ret, _, err := procEmptyClipboard.Call()
	if ret == 0 {
		return fmt.Errorf("EmptyClipboard: %w", err)
	}
	ret, _, err = procSetClipboardData.Call(cfUnicodeText, handle)
	if ret == 0 {
		return fmt.Errorf("SetClipboardData: %w", err)
	}
	clipboardOwnsHandle = true
	return nil
}

func openClipboard() error {
	ret, _, err := procOpenClipboard.Call(0)
	if ret == 0 {
		return fmt.Errorf("OpenClipboard: %w", err)
	}
	return nil
}

func captureScreenPNG(budget int) ([]byte, error) {
	width, _, _ := procGetSystemMetrics.Call(smCXScreen)
	height, _, _ := procGetSystemMetrics.Call(smCYScreen)
	if width == 0 || height == 0 {
		return nil, fmt.Errorf("desktop_session_unavailable: screen size is zero")
	}
	screenDC, _, err := procGetDC.Call(0)
	if screenDC == 0 {
		return nil, fmt.Errorf("GetDC: %w", err)
	}
	defer procReleaseDC.Call(0, screenDC)
	memDC, _, err := procCreateCompatibleDC.Call(screenDC)
	if memDC == 0 {
		return nil, fmt.Errorf("CreateCompatibleDC: %w", err)
	}
	defer procDeleteDC.Call(memDC)
	bitmap, _, err := procCreateCompatibleBMP.Call(screenDC, width, height)
	if bitmap == 0 {
		return nil, fmt.Errorf("CreateCompatibleBitmap: %w", err)
	}
	defer procDeleteObject.Call(bitmap)
	old, _, _ := procSelectObject.Call(memDC, bitmap)
	if old != 0 {
		defer procSelectObject.Call(memDC, old)
	}
	ret, _, err := procBitBlt.Call(memDC, 0, 0, width, height, screenDC, 0, 0, srccopy)
	if ret == 0 {
		return nil, fmt.Errorf("BitBlt: %w", err)
	}
	w, h := int(width), int(height)
	pixels := make([]byte, w*h*4)
	bi := bitmapInfo{}
	bi.Header.Size = uint32(unsafe.Sizeof(bi.Header))
	bi.Header.Width = int32(w)
	bi.Header.Height = -int32(h)
	bi.Header.Planes = 1
	bi.Header.BitCount = 32
	bi.Header.Compression = biRGB
	ret, _, err = procGetDIBits.Call(memDC, bitmap, 0, uintptr(h), uintptr(unsafe.Pointer(&pixels[0])), uintptr(unsafe.Pointer(&bi)), dibRGBColors)
	if ret == 0 {
		return nil, fmt.Errorf("GetDIBits: %w", err)
	}
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for i := 0; i < w*h; i++ {
		b := pixels[i*4]
		g := pixels[i*4+1]
		r := pixels[i*4+2]
		a := pixels[i*4+3]
		if a == 0 {
			a = 255
		}
		img.Pix[i*4] = r
		img.Pix[i*4+1] = g
		img.Pix[i*4+2] = b
		img.Pix[i*4+3] = a
	}
	return encodePNGWithinBudget(img, budget)
}

func pngBudget(maxOutputBytes, frames int) int {
	budget := 40000
	if maxOutputBytes > 0 && maxOutputBytes/2 < budget {
		budget = maxOutputBytes / 2
	}
	if frames > 1 {
		budget /= frames
	}
	if budget < 4000 {
		budget = 4000
	}
	return budget
}
