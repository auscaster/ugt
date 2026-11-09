// Package linkhandler makes the system open ugt:// links with ugt, as web
// pages' "Open in UGT" links do, for this user alone and with no admin
// rights: a small app in ~/Applications on a Mac, a key under the user's
// classes on Windows, and a desktop entry on Linux. It implements the
// LinkHandler port.
package linkhandler

import (
	"github.com/usegeekthing/ugt/internal/app"
	"github.com/usegeekthing/ugt/internal/domain/link"
)

var _ app.LinkHandler = (*Handler)(nil)

// scheme is the one scheme the handler takes.
const scheme = link.Scheme
