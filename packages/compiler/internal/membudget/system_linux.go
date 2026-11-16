package membudget

import "os"

func readSystem() system {
	return linuxSystem(os.ReadFile, "/sys/fs/cgroup")
}
