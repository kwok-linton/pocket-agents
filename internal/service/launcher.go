package service

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// The launcher accepts no URL arguments: untrusted links cannot choose commands,
// data directories, or destinations. Authentication stays in the local owner CLI.
func installLauncher(home, binary, data string) error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	root := filepath.Join(home, "Applications", "Pocket Agents.app", "Contents")
	if err := os.MkdirAll(filepath.Join(root, "MacOS"), 0755); err != nil {
		return err
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?><plist version="1.0"><dict>
<key>CFBundleIdentifier</key><string>local.pocket-agents.launcher</string>
<key>CFBundleName</key><string>Pocket Agents</string>
<key>CFBundleExecutable</key><string>console</string>
<key>CFBundlePackageType</key><string>APPL</string>
<key>LSUIElement</key><true/>
<key>CFBundleURLTypes</key><array><dict><key>CFBundleURLName</key><string>Open local console</string><key>CFBundleURLSchemes</key><array><string>pocket-agents</string></array></dict></array>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(root, "Info.plist"), []byte(plist), 0644); err != nil {
		return err
	}
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := fmt.Sprintf("#!/bin/sh\nexec %s open --data %s\n", quote(binary), quote(data))
	if err := os.WriteFile(filepath.Join(root, "MacOS", "console"), []byte(script), 0755); err != nil {
		return err
	}
	return exec.Command("/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister", "-f", filepath.Dir(root)).Run()
}
