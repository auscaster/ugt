package recipe

import (
	"slices"
	"testing"
)

func TestUnpinnedBases(t *testing.T) {
	dockerfile := `FROM --platform=$BUILDPLATFORM golang:1.25-bookworm@sha256:` + "aa" + ` AS build
FROM build AS test
from scratch
FROM ${BASE}
FROM debian:bookworm-slim
FROM node:22-bookworm-slim@sha256:bb
`
	if got, want := unpinnedBases(dockerfile), []string{"${BASE}", "debian:bookworm-slim"}; !slices.Equal(got, want) {
		t.Fatalf("unpinnedBases = %q, want %q", got, want)
	}
}
