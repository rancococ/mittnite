//go:build !linux

package proc

// ReapZombies is only meaningful when mittnite runs as PID 1 on Linux; on
// other platforms it does nothing.
func ReapZombies() {}
