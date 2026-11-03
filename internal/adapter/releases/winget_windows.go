//go:build windows

package releases

import (
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// uninstallKeys are where winget records each portable package it installs,
// as <package>_<source>, for one user and for the machine.
var uninstallKeys = []struct {
	root registry.Key
	path string
}{
	{registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
	{registry.LOCAL_MACHINE, `Software\Microsoft\Windows\CurrentVersion\Uninstall`},
	{registry.LOCAL_MACHINE, `Software\Wow6432Node\Microsoft\Windows\CurrentVersion\Uninstall`},
}

// wingetRecords says whether winget's own record of ugt's package names self,
// as the file it installed or the link it made, wherever it was installed.
func wingetRecords(self string) bool {
	for _, u := range uninstallKeys {
		k, err := registry.OpenKey(u.root, u.path, registry.ENUMERATE_SUB_KEYS)
		if err != nil {
			continue
		}
		names, _ := k.ReadSubKeyNames(-1)
		k.Close()
		for _, name := range names {
			if strings.HasPrefix(strings.ToLower(name), wingetID+"_") && recordNames(u.root, u.path+`\`+name, self) {
				return true
			}
		}
	}
	return false
}

// recordNames says whether one package's record names self.
func recordNames(root registry.Key, path, self string) bool {
	k, err := registry.OpenKey(root, path, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	for _, value := range []string{"TargetFullPath", "SymlinkFullPath"} {
		if p, _, err := k.GetStringValue(value); err == nil && strings.EqualFold(filepath.Clean(p), filepath.Clean(self)) {
			return true
		}
	}
	return false
}
