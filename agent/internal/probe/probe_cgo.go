//go:build probe_cgo

package probe

/*
#cgo LDFLAGS: -L${SRCDIR}/../../rust/target/debug -lpingsanto_probe
unsigned int pingsanto_probe_version();
*/
import "C"

func LibraryVersion() uint32 {
	return uint32(C.pingsanto_probe_version())
}
