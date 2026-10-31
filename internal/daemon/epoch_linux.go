package daemon

import (
	"errors"
	"os"
	"strconv"
	"strings"
)

// ProcessEpoch requires an operator-maintained PID file for the same local core
// that owns the configured API endpoint. It never guesses a PID from uptime.
func ProcessEpoch(pidFile string) (string, error) {
	info, err := os.Lstat(pidFile)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0022 != 0 || info.Size() > 64 {
		return "", errors.New("trusted Xray PID file unavailable/unsafe")
	}
	b, err := os.ReadFile(pidFile)
	if err != nil {
		return "", errors.New("cannot read Xray PID file")
	}
	pid, err := strconv.ParseInt(strings.TrimSpace(string(b)), 10, 32)
	if err != nil || pid <= 0 {
		return "", errors.New("invalid Xray PID")
	}
	stat, err := os.ReadFile("/proc/" + strconv.FormatInt(pid, 10) + "/stat")
	if err != nil {
		return "", errors.New("Xray process unavailable")
	}
	end := strings.LastIndex(string(stat), ")")
	if end < 0 {
		return "", errors.New("invalid Xray process stat")
	}
	fields := strings.Fields(string(stat[end+1:]))
	if len(fields) < 20 || fields[0] == "Z" {
		return "", errors.New("Xray process not running")
	}
	if _, err := strconv.ParseUint(fields[19], 10, 64); err != nil {
		return "", errors.New("invalid Xray process start time")
	}
	boot, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return "", errors.New("boot identity unavailable")
	}
	return strings.TrimSpace(string(boot)) + ":" + strconv.FormatInt(pid, 10) + ":" + fields[19], nil
}
