//go:build linux

package metrics

import "os"

func readFDCount() (int, string) {
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		return 0, "unsupported"
	}
	return len(entries), "linux"
}
