// This test opener records the actual URL or folder the helper asks the OS to open.
package main

import (
	"os"
)

func main() {
	if len(os.Args) != 2 || os.Getenv("UGT_CAPTURE") == "" {
		os.Exit(2)
	}
	if err := os.WriteFile(os.Getenv("UGT_CAPTURE"), []byte(os.Args[1]), 0o600); err != nil {
		panic(err)
	}
}
