package proc

import "sync"

// managedPids maps the main pid of every running job process to its job
// name. The zombie reaper uses it to leave those processes' exit statuses to
// the job supervisor's cmd.Wait(), and to attribute reaped orphans to the
// job whose process group they belonged to.
var managedPids sync.Map // int → string

func registerManagedPid(pid int, jobName string) {
	managedPids.Store(pid, jobName)
}

func unregisterManagedPid(pid int) {
	managedPids.Delete(pid)
}

func lookupManagedPid(pid int) (string, bool) {
	jobName, ok := managedPids.Load(pid)
	if !ok {
		return "", false
	}
	return jobName.(string), true
}
