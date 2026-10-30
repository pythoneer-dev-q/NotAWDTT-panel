package server

import (
	"os"
	"strconv"
	"strings"
	"sync"
)

var nodeCPUState struct {
	sync.Mutex
	total uint64
	idle  uint64
}

func nodeHostMetrics() (cpu float64, memoryUsed, memoryTotal uint64) {
	if raw, err := os.ReadFile("/proc/stat"); err == nil {
		line := strings.SplitN(string(raw), "\n", 2)[0]
		parts := strings.Fields(line)
		if len(parts) >= 5 && parts[0] == "cpu" {
			var total, idle uint64
			for i, part := range parts[1:] {
				v, err := strconv.ParseUint(part, 10, 64)
				if err != nil {
					break
				}
				total += v
				if i == 3 || i == 4 {
					idle += v
				}
			}
			nodeCPUState.Lock()
			if nodeCPUState.total > 0 && total > nodeCPUState.total && idle >= nodeCPUState.idle {
				delta := total - nodeCPUState.total
				busy := delta - minUint64(delta, idle-nodeCPUState.idle)
				cpu = 100 * float64(busy) / float64(delta)
			}
			nodeCPUState.total, nodeCPUState.idle = total, idle
			nodeCPUState.Unlock()
		}
	}
	if raw, err := os.ReadFile("/proc/meminfo"); err == nil {
		var available uint64
		for _, line := range strings.Split(string(raw), "\n") {
			parts := strings.Fields(line)
			if len(parts) < 2 {
				continue
			}
			v, err := strconv.ParseUint(parts[1], 10, 64)
			if err != nil {
				continue
			}
			switch parts[0] {
			case "MemTotal:":
				memoryTotal = v * 1024
			case "MemAvailable:":
				available = v * 1024
			}
		}
		if available <= memoryTotal {
			memoryUsed = memoryTotal - available
		}
	}
	return
}

func minUint64(a, b uint64) uint64 {
	if a < b {
		return a
	}
	return b
}
