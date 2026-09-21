//go:build windows

package service

import "fmt"

// shmFreeBytes has no Windows equivalent of /dev/shm — cloneTempBase's os.Stat("/dev/shm") check
// always fails first on Windows, so this is never actually called there; it exists only so the
// package compiles.
func shmFreeBytes(path string) (uint64, error) {
	return 0, fmt.Errorf("shmFreeBytes: not supported on windows")
}
