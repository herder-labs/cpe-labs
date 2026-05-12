//go:build !linux

package metrics

func readFDCount() (int, string) {
	return 0, "unsupported"
}
