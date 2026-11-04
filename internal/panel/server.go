package panel

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"time"
)

type resourceUsage struct {
	Total int64 `json:"total"`
	Used  int64 `json:"used"`
}

type containerStat struct {
	Name    string  `json:"name"`
	Image   string  `json:"image"`
	CPUPct  float64 `json:"cpuPct"`
	MemUsed int64   `json:"memUsed"`
	MemPct  float64 `json:"memPct"`
}

type serverInfo struct {
	Hostname   string          `json:"hostname"`
	OS         string          `json:"os"`
	Uptime     int64           `json:"uptime"`
	Load       []float64       `json:"load"`
	CPUCount   int             `json:"cpuCount"`
	CPUPct     float64         `json:"cpuPct"`
	Mem        resourceUsage   `json:"mem"`
	Disk       resourceUsage   `json:"disk"`
	Docker     string          `json:"docker"`
	Containers []containerStat `json:"containers"`
}

func (p *Panel) handleServer(w http.ResponseWriter, r *http.Request) {
	// The box view lists every container across teams, so it's for whoever runs the box — no team's owner or member may enumerate other teams' apps.
	if !p.isInstanceAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	host, _ := os.Hostname()
	info := serverInfo{
		Hostname:   host,
		OS:         runtime.GOOS + "/" + runtime.GOARCH,
		CPUCount:   runtime.NumCPU(),
		Uptime:     procUptime(),
		Load:       procLoadAvg(),
		CPUPct:     cpuPercent(),
		Mem:        procMem(),
		Disk:       diskUsage(),
		Docker:     dockerVersion(),
		Containers: dockerStats(),
	}
	if info.Load == nil {
		info.Load = []float64{}
	}
	if info.Containers == nil {
		info.Containers = []containerStat{}
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(info)
}

func procUptime() int64 {
	b, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	f := strings.Fields(string(b))
	if len(f) == 0 {
		return 0
	}
	up, _ := strconv.ParseFloat(f[0], 64)
	return int64(up)
}

func procLoadAvg() []float64 {
	b, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return nil
	}
	f := strings.Fields(string(b))
	out := make([]float64, 0, 3)
	for i := 0; i < 3 && i < len(f); i++ {
		v, _ := strconv.ParseFloat(f[i], 64)
		out = append(out, v)
	}
	return out
}

func procMem() resourceUsage {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return resourceUsage{}
	}
	defer f.Close()
	var total, avail int64
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "MemTotal:"):
			total = meminfoBytes(line)
		case strings.HasPrefix(line, "MemAvailable:"):
			avail = meminfoBytes(line)
		}
	}
	if total == 0 {
		return resourceUsage{}
	}
	return resourceUsage{Total: total, Used: total - avail}
}

func meminfoBytes(line string) int64 {
	f := strings.Fields(line) // ["MemTotal:", "16384000", "kB"]
	if len(f) < 2 {
		return 0
	}
	kb, _ := strconv.ParseInt(f[1], 10, 64)
	return kb * 1024
}

func diskUsage() resourceUsage {
	var st syscall.Statfs_t
	if err := syscall.Statfs("/", &st); err != nil {
		return resourceUsage{}
	}
	bsize := int64(st.Bsize)
	total := int64(st.Blocks) * bsize
	free := int64(st.Bavail) * bsize
	return resourceUsage{Total: total, Used: total - free}
}

type cpuTimes struct{ total, idle int64 }

func cpuPercent() float64 {
	a, ok := cpuSample()
	if !ok {
		return 0
	}
	time.Sleep(200 * time.Millisecond)
	b, ok := cpuSample()
	if !ok {
		return 0
	}
	totalDelta := float64(b.total - a.total)
	idleDelta := float64(b.idle - a.idle)
	if totalDelta <= 0 {
		return 0
	}
	pct := (1 - idleDelta/totalDelta) * 100
	if pct < 0 {
		return 0
	}
	return pct
}

func cpuSample() (cpuTimes, bool) {
	b, err := os.ReadFile("/proc/stat")
	if err != nil {
		return cpuTimes{}, false
	}
	for _, line := range strings.Split(string(b), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		var t cpuTimes
		for i, v := range strings.Fields(line)[1:] {
			n, _ := strconv.ParseInt(v, 10, 64)
			t.total += n
			if i == 3 || i == 4 { // idle + iowait
				t.idle += n
			}
		}
		return t, true
	}
	return cpuTimes{}, false
}

func dockerVersion() string {
	out, err := exec.Command("docker", "version", "--format", "{{.Server.Version}}").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func dockerStats() []containerStat {
	images := map[string]string{}
	if out, err := exec.Command("docker", "ps", "--format", "{{.Names}}\t{{.Image}}").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if p := strings.SplitN(line, "\t", 2); len(p) == 2 {
				images[p[0]] = p[1]
			}
		}
	}
	out, err := exec.Command("docker", "stats", "--no-stream", "--format",
		"{{.Name}}\t{{.CPUPerc}}\t{{.MemUsage}}\t{{.MemPerc}}").Output()
	if err != nil {
		return nil
	}
	var cs []containerStat
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		f := strings.Split(line, "\t")
		if len(f) < 4 {
			continue
		}
		cs = append(cs, containerStat{
			Name:    f[0],
			Image:   images[f[0]],
			CPUPct:  parsePct(f[1]),
			MemUsed: parseMemUsage(f[2]),
			MemPct:  parsePct(f[3]),
		})
	}
	return cs
}

func parsePct(s string) float64 {
	v, _ := strconv.ParseFloat(strings.TrimSuffix(strings.TrimSpace(s), "%"), 64)
	return v
}

func parseMemUsage(s string) int64 {
	// e.g. "12.3MiB / 3.8GiB" — take the used side.
	if i := strings.IndexByte(s, '/'); i >= 0 {
		s = s[:i]
	}
	return parseBytes(strings.TrimSpace(s))
}

func parseBytes(s string) int64 {
	units := []struct {
		suf string
		m   int64
	}{
		{"GiB", 1 << 30}, {"MiB", 1 << 20}, {"KiB", 1 << 10},
		{"GB", 1000000000}, {"MB", 1000000}, {"kB", 1000}, {"B", 1},
	}
	mult := int64(1)
	for _, u := range units {
		if strings.HasSuffix(s, u.suf) {
			s = strings.TrimSpace(strings.TrimSuffix(s, u.suf))
			mult = u.m
			break
		}
	}
	v, _ := strconv.ParseFloat(s, 64)
	return int64(v * float64(mult))
}
