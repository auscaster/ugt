//go:build windows

package releases

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// A ugt winget installed where the user chose (winget install --location) is
// known by winget's own record of it.
func TestManagerKnowsWingetByItsRecord(t *testing.T) {
	path := `Software\ugt-test-` + t.Name() + `\Uninstall`
	saved := uninstallKeys
	uninstallKeys = []struct {
		root registry.Key
		path string
	}{{registry.CURRENT_USER, path}}
	t.Cleanup(func() {
		uninstallKeys = saved
		registry.DeleteKey(registry.CURRENT_USER, path+`\auscaster.ugt_Microsoft.Winget.Source_8wekyb3d8bbwe`)
		registry.DeleteKey(registry.CURRENT_USER, path)
		registry.DeleteKey(registry.CURRENT_USER, `Software\ugt-test-`+t.Name())
	})
	k, _, err := registry.CreateKey(registry.CURRENT_USER, path+`\auscaster.ugt_Microsoft.Winget.Source_8wekyb3d8bbwe`, registry.SET_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	k.SetStringValue("TargetFullPath", `D:\Tools\ugt\ugt.exe`)
	k.Close()
	if got := (&Releases{Self: `D:\Tools\ugt\ugt.exe`}).Manager(); got != "winget upgrade auscaster.ugt" {
		t.Errorf("winget's ugt: %q", got)
	}
	if got := (&Releases{Self: `D:\Tools\other\ugt.exe`}).Manager(); got != "" {
		t.Errorf("a ugt winget did not install: %q", got)
	}
}
