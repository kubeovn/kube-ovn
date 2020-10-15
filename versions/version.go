package versions

import (
	"fmt"
	"runtime"
	"time"
)

var (
	COMMIT  = "unknown"
	VERSION = "unknown"
)

func String() string {
	return fmt.Sprintf(`
-------------------------------------------------------------------------------
Kube-OVN: 
  Version:       %v
  Build:         %v
  Commit:        %v
  Go Version:    %v
  Arch:          %v
-------------------------------------------------------------------------------
`, VERSION, time.Now().String(), COMMIT, runtime.Version(), runtime.GOARCH)
}
