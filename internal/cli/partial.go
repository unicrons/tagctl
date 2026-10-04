package cli

import (
	"fmt"
	"io"

	"github.com/unicrons/tagctl/internal/types"
)

// warnPartialScan tells the user that the scan read from path missed resources
// and what that skews.
func warnPartialScan(w io.Writer, path string, scan *types.ScanResult, consequence string) {
	if !scan.Partial {
		return
	}
	fmt.Fprintf(w, "Warning: %s is a partial scan (%d provider(s) failed discovery); %s\n", printable(path), len(scan.Errors), consequence)
}
