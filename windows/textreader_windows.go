//go:build windows

package main

import (
	"context"
	"os"
	"os/exec"
	"syscall"
	"time"
)

// The text in the close-up, read on this machine by the OCR that ships with Windows 10 and 11
// (Windows.Media.Ocr), like the Mac's TextReader. That engine is only reachable through WinRT, and calling
// WinRT from plain syscalls means building its async and buffer interfaces by hand, so a small script in
// Windows PowerShell, which every Windows has, does the reading instead. Nothing leaves the machine.

// The path comes in through SMUGSHOT_OCR_PATH, never inside the script, so no file name can change what runs.
// Exit 3: no OCR language is installed for the user's languages.
const ocrScript = `[Console]::OutputEncoding = [Text.Encoding]::UTF8
$null = [Windows.Media.Ocr.OcrEngine, Windows.Foundation, ContentType=WindowsRuntime]
$null = [Windows.Storage.StorageFile, Windows.Storage, ContentType=WindowsRuntime]
$null = [Windows.Graphics.Imaging.BitmapDecoder, Windows.Graphics, ContentType=WindowsRuntime]
Add-Type -AssemblyName System.Runtime.WindowsRuntime
$asTask = ([System.WindowsRuntimeSystemExtensions].GetMethods() | Where-Object { $_.Name -eq 'AsTask' -and $_.GetParameters().Count -eq 1 -and $_.GetParameters()[0].ParameterType.Name -eq 'IAsyncOperation` + "`" + `1' })[0]
function Await($op, $type) { $task = $asTask.MakeGenericMethod($type).Invoke($null, @($op)); $task.Wait(); $task.Result }
$engine = [Windows.Media.Ocr.OcrEngine]::TryCreateFromUserProfileLanguages()
if ($null -eq $engine) { exit 3 }
$file = Await ([Windows.Storage.StorageFile]::GetFileFromPathAsync($env:SMUGSHOT_OCR_PATH)) ([Windows.Storage.StorageFile])
$stream = Await ($file.OpenAsync([Windows.Storage.FileAccessMode]::Read)) ([Windows.Storage.Streams.IRandomAccessStream])
$decoder = Await ([Windows.Graphics.Imaging.BitmapDecoder]::CreateAsync($stream)) ([Windows.Graphics.Imaging.BitmapDecoder])
$bitmap = Await ($decoder.GetSoftwareBitmapAsync()) ([Windows.Graphics.Imaging.SoftwareBitmap])
$result = Await ($engine.RecognizeAsync($bitmap)) ([Windows.Media.Ocr.OcrResult])
foreach ($line in $result.Lines) { $line.Text }
`

// readText returns the lines of text in the picture at cropPath, top to bottom. Anything that goes wrong (no
// OCR language, no PowerShell, more than six seconds) gives no lines: the smugshot is still good without them.
func readText(cropPath string) []string {
	ctx, stop := context.WithTimeout(context.Background(), 6*time.Second)
	defer stop()
	cmd := exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass", "-Command", ocrScript)
	cmd.Env = append(os.Environ(), "SMUGSHOT_OCR_PATH="+cropPath)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000} // CREATE_NO_WINDOW: no console flashes up
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return ocrLines(string(out))
}
