// Command sign writes and signs a release's list in the folder of its files,
// with the release key's private half in UGT_RELEASE_KEY:
//
//	go run ./internal/devtools/release/sign -version 0.2.0 -dir dist
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/auscaster/ugt/internal/devtools/release"
	domain "github.com/auscaster/ugt/internal/domain/release"
)

func main() {
	version := flag.String("version", "", "the release's semantic version, without a leading v")
	dir := flag.String("dir", "", "the folder of the release's files")
	flag.Parse()
	if *version == "" || *dir == "" {
		flag.Usage()
		os.Exit(2)
	}
	key, err := release.Key(os.Getenv("UGT_RELEASE_KEY"))
	if err == nil {
		err = release.Write(*dir, *version, key, domain.Key)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "sign:", err)
		os.Exit(1)
	}
}
