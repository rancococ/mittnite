//go:build !(linux && (amd64 || arm64))

package proc

// ReapZombies is only meaningful when mittnite runs as PID 1 on Linux, and
// the reaper's siginfo layout is only implemented for 64-bit architectures;
// everywhere else it does nothing.
func ReapZombies() {}
