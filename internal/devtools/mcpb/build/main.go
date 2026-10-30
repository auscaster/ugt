// Command build writes UGT's MCP bundle for one operating system and CPU:
//
//	go run ./internal/devtools/mcpb/build -os windows -arch amd64 -binary ugt.exe -version 0.1.0 -out dist
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"

	"github.com/auscaster/ugt/internal/devtools/mcpb"
)

func main() {
	goos := flag.String("os", "", "darwin, windows or linux")
	goarch := flag.String("arch", "", "amd64 or arm64")
	binary := flag.String("binary", "", "the ugt binary built for that system")
	version := flag.String("version", "", "the release's semantic version")
	out := flag.String("out", ".", "the folder to write the bundle to")
	flag.Parse()
	if *goos == "" || *goarch == "" || *binary == "" || *version == "" {
		flag.Usage()
		os.Exit(2)
	}
	if err := build(*goos, *goarch, *binary, *version, *out); err != nil {
		fmt.Fprintln(os.Stderr, "mcpb:", err)
		os.Exit(1)
	}
}

func build(goos, goarch, binary, version, out string) error {
	tools, err := mcpb.Tools(context.Background())
	if err != nil {
		return err
	}
	if err := os.MkdirAll(out, 0o755); err != nil {
		return err
	}
	path := filepath.Join(out, mcpb.Name(goos, goarch))
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	if err := mcpb.Build(f, binary, goos, version, tools); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	fmt.Println(path)
	return nil
}
