//go:build !probe_cgo

package probe

func LibraryVersion() uint32 {
	return 0
}
