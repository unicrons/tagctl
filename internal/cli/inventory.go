package cli

import (
	"fmt"
	"io"

	"github.com/unicrons/tagctl/internal/types"
)

// warnNoInventory tells the user that the scan read from path counts resources
// but names none of them, so diff cannot tell which ones appeared or disappeared.
func warnNoInventory(w io.Writer, path string, scan *types.ScanResult) {
	if scan.TotalResources == 0 || len(scan.Resources) > 0 || len(scan.Findings) > 0 || len(scan.Violations) > 0 {
		return
	}
	fmt.Fprintf(w, "Warning: %s counts %d resource(s) but names none (no inventory and no findings); new and removed resources may be wrong\n", printable(path), scan.TotalResources)
}
