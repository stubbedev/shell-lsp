package main

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

const (
	rssInterval      = 250 * time.Millisecond
	procStatPgrpAt   = 2
	procStatRSSAt    = 21
	statusPeakPrefix = "VmHWM:"
	kibibyte         = 1024
)

var errRSSUnsupported = errors.New("this platform has no process memory sampler")

type rssSampler struct {
	stop chan struct{}
	done chan struct{}
	peak *int64
	err  error
}

func startRSSSampler(group int) *rssSampler {
	r := &rssSampler{stop: make(chan struct{}), done: make(chan struct{})}
	go r.run(group)
	return r
}

func (r *rssSampler) run(group int) {
	defer close(r.done)
	ticker := time.NewTicker(rssInterval)
	defer ticker.Stop()
	for {
		if !r.sample(group) {
			return
		}
		select {
		case <-r.stop:
			return
		case <-ticker.C:
		}
	}
}

func (r *rssSampler) sample(group int) bool {
	rss, err := sampleTreeRSS(group)
	if err != nil {
		r.err = err
		return false
	}
	if rss > 0 && (r.peak == nil || rss > *r.peak) {
		r.peak = &rss
	}
	return true
}

func (r *rssSampler) finish(server string) *int64 {
	close(r.stop)
	<-r.done
	if r.err != nil && !errors.Is(r.err, errRSSUnsupported) {
		fmt.Fprintf(os.Stderr, "nixparity: sampling the memory of %s: %v\n", server, r.err)
	}
	return r.peak
}

type procStat struct {
	group    int
	rssPages int64
}

func parseProcStat(text string) (procStat, error) {
	_, afterName, named := strings.CutLast(text, ")")
	if !named {
		return procStat{}, fmt.Errorf("process status %q has no command name", text)
	}
	fields := strings.Fields(afterName)
	if len(fields) <= procStatRSSAt {
		return procStat{}, fmt.Errorf("process status %q has %d fields after the command name", text, len(fields))
	}
	group, err := strconv.Atoi(fields[procStatPgrpAt])
	if err != nil {
		return procStat{}, fmt.Errorf("reading the process group of %q: %w", text, err)
	}
	rss, err := strconv.ParseInt(fields[procStatRSSAt], 10, 64)
	if err != nil {
		return procStat{}, fmt.Errorf("reading the resident pages of %q: %w", text, err)
	}
	return procStat{group: group, rssPages: rss}, nil
}

func parsePeakRSS(status string) (int64, error) {
	for line := range strings.SplitSeq(status, "\n") {
		rest, ok := strings.CutPrefix(line, statusPeakPrefix)
		if !ok {
			continue
		}
		fields := strings.Fields(rest)
		if len(fields) != 2 || fields[1] != "kB" {
			return 0, fmt.Errorf("process status line %q is not a size in kB", line)
		}
		kb, err := strconv.ParseInt(fields[0], 10, 64)
		if err != nil {
			return 0, fmt.Errorf("reading the size in %q: %w", line, err)
		}
		return kb * kibibyte, nil
	}
	return 0, nil
}
