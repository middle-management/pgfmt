//go:build (!js || wasip1) && !tinygo

package printer

import (
	"fmt"
	"os"
)

func warn(format string, args ...any) {
	fmt.Fprintf(os.Stderr, format+"\n", args...)
}
