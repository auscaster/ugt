//go:build !windows

package releases

// wingetRecords: winget is Windows'.
func wingetRecords(string) bool { return false }
