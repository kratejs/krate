package sysinfo

// sampler measures the CPU load between two calls to sample.
type sampler struct {
	prev cpuTimes
}

type cpuTimes struct{ busy, total uint64 }

func newSampler() (*sampler, error) {
	t, err := readCPUTimes()
	if err != nil {
		return nil, err
	}
	return &sampler{prev: t}, nil
}

func (s *sampler) sample() (float64, error) {
	t, err := readCPUTimes()
	if err != nil {
		return 0, err
	}
	load := loadBetween(s.prev, t)
	s.prev = t
	return load, nil
}

// loadBetween is the busy share of the time between two readings, in percent.
func loadBetween(a, b cpuTimes) float64 {
	if b.total <= a.total || b.busy < a.busy {
		return 0
	}
	v := 100 * float64(b.busy-a.busy) / float64(b.total-a.total)
	if v > 100 {
		return 100
	}
	return v
}
