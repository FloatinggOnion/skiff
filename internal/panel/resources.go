package panel

import (
	"encoding/json"
	"math"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
)

const (
	resWindowSecs  int64 = 24 * 60 * 60
	resBucketSecs  int64 = 60
	resSampleEvery       = 10 * time.Second
	resSettle            = 12 * time.Second
)

type resBucket struct {
	T        int64   `json:"t"`
	CPUSum   float64 `json:"cpu"`
	MemSum   int64   `json:"mem"`
	MemLimit int64   `json:"lim"`
	N        int     `json:"n"`
	Restarts int     `json:"rs"`
}

type resStore struct {
	mu      sync.Mutex
	apps    map[string]map[int64]*resBucket
	last    map[string]int
	updated int64
	file    string
}

var resStats *resStore

func newResStore(file string) *resStore {
	s := &resStore{apps: map[string]map[int64]*resBucket{}, last: map[string]int{}, file: file}
	s.load()
	return s
}

func (s *resStore) record(samples []docker.ContainerResource) {
	type acc struct {
		cpu      float64
		mem, lim int64
		restarts int
	}
	byApp := map[string]*acc{}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, r := range samples {
		a := byApp[r.App]
		if a == nil {
			a = &acc{}
			byApp[r.App] = a
		}
		a.cpu += r.CPUPerc
		a.mem += r.MemBytes
		if r.MemLimit > a.lim {
			a.lim = r.MemLimit
		}
		if prev, seen := s.last[r.Container]; seen && r.Restarts > prev {
			a.restarts += r.Restarts - prev
		}
		s.last[r.Container] = r.Restarts
	}

	t := (time.Now().Unix() / resBucketSecs) * resBucketSecs
	for app, a := range byApp {
		buckets := s.apps[app]
		if buckets == nil {
			buckets = map[int64]*resBucket{}
			s.apps[app] = buckets
		}
		b := buckets[t]
		if b == nil {
			b = &resBucket{T: t}
			buckets[t] = b
		}
		b.CPUSum += a.cpu
		b.MemSum += a.mem
		if a.lim > b.MemLimit {
			b.MemLimit = a.lim
		}
		b.N++
		b.Restarts += a.restarts
	}
	s.updated = time.Now().Unix()
	s.prune(time.Now().Unix())
}

func (s *resStore) prune(now int64) {
	cutoff := now - resWindowSecs
	for app, buckets := range s.apps {
		for t := range buckets {
			if t < cutoff {
				delete(buckets, t)
			}
		}
		if len(buckets) == 0 {
			delete(s.apps, app)
		}
	}
}

type resSnapshot struct {
	Updated int64                  `json:"updated"`
	Apps    map[string][]resBucket `json:"apps"`
}

func (s *resStore) snapshot() {
	s.mu.Lock()
	snap := resSnapshot{Updated: s.updated, Apps: map[string][]resBucket{}}
	for app, buckets := range s.apps {
		arr := make([]resBucket, 0, len(buckets))
		for _, b := range buckets {
			arr = append(arr, *b)
		}
		sort.Slice(arr, func(i, j int) bool { return arr[i].T < arr[j].T })
		snap.Apps[app] = arr
	}
	s.mu.Unlock()
	if s.file == "" {
		return
	}
	b, err := json.Marshal(snap)
	if err != nil {
		return
	}
	tmp := s.file + ".tmp"
	if os.WriteFile(tmp, b, 0o644) == nil {
		_ = os.Rename(tmp, s.file)
	}
}

func (s *resStore) load() {
	if s.file == "" {
		return
	}
	b, err := os.ReadFile(s.file)
	if err != nil {
		return
	}
	var snap resSnapshot
	if json.Unmarshal(b, &snap) != nil {
		return
	}
	s.updated = snap.Updated
	for app, arr := range snap.Apps {
		buckets := map[int64]*resBucket{}
		for i := range arr {
			bb := arr[i]
			buckets[bb.T] = &bb
		}
		s.apps[app] = buckets
	}
}

func (p *Panel) resourceLoop() {
	time.Sleep(resSettle)
	tick := time.NewTicker(resSampleEvery)
	defer tick.Stop()
	for {
		guard("resourceLoop", func() {
			if samples, err := p.eng.StatsFor(p.resourceTargets()); err == nil && len(samples) > 0 {
				resStats.record(samples)
			}
			resStats.snapshot()
		})
		<-tick.C
	}
}

// resourceTargets is every container worth sampling: apps under their name, managed databases and buckets under db:<id> and bucket:<id>.
func (p *Panel) resourceTargets() map[string]string {
	keys, err := p.eng.AppContainerKeys()
	if err != nil {
		keys = map[string]string{}
	}
	// docker stats fails outright on a missing container, so only add ones that are running.
	running := map[string]bool{}
	if names, err := p.eng.RunningContainers(); err == nil {
		for _, n := range names {
			running[n] = true
		}
	}
	for _, d := range allDatabases() {
		if running[d.Container] {
			keys[d.Container] = "db:" + d.ID
		}
	}
	for _, b := range allBuckets() {
		if running[b.Container] {
			keys[b.Container] = "bucket:" + b.ID
		}
	}
	return keys
}

type resourcesSeries struct {
	T   int64    `json:"t"`
	CPU *float64 `json:"cpu"`
	Mem *int64   `json:"mem"`
}

type resourcesApp struct {
	Name string  `json:"name"`
	CPU  float64 `json:"cpu"`
	Mem  int64   `json:"mem"`
}

type resourcesResponse struct {
	RangeMins  int               `json:"rangeMins"`
	BucketSecs int               `json:"bucketSecs"`
	CurCPU     float64           `json:"curCpu"`
	CurMem     int64             `json:"curMem"`
	PeakCPU    float64           `json:"peakCpu"`
	PeakMem    int64             `json:"peakMem"`
	MemLimit   int64             `json:"memLimit"`
	Restarts   int               `json:"restarts"`
	Samples    int               `json:"samples"`
	Series     []resourcesSeries `json:"series"`
	Apps       []resourcesApp    `json:"apps"`
	AppOptions []string          `json:"appOptions"`
	Updated    int64             `json:"updated"`
}

func round2(v float64) float64 { return math.Round(v*100) / 100 }

func (p *Panel) handleResources(w http.ResponseWriter, r *http.Request) {
	team := p.teamID(r)
	only := sanitizeName(r.URL.Query().Get("app")) // "" = all team apps
	rangeMins := clampRange(r.URL.Query().Get("range"))
	bucketSecs := displayBucket(rangeMins)

	now := time.Now().Unix()
	startT := ((now - int64(rangeMins)*60) / bucketSecs) * bucketSecs
	nowT := (now / bucketSecs) * bucketSecs

	type agg struct {
		cpuSum float64
		memSum int64
		n      int
	}
	buckets := map[int64]*agg{}
	appCur := map[string]*resourcesApp{}
	appLatestT := map[string]int64{}
	appOptions := []string{}
	var resp resourcesResponse

	// Copy the sampler's buckets under the lock, then do per-app team filtering (which hits the DB) with the lock released, so a dashboard request never stalls the sampler/autoscaler sharing this mutex.
	type appSnap struct {
		app string
		bs  map[int64]resBucket
	}
	var snaps []appSnap
	resStats.mu.Lock()
	for app, bs := range resStats.apps {
		cp := make(map[int64]resBucket, len(bs))
		for t, b := range bs {
			cp[t] = *b
		}
		snaps = append(snaps, appSnap{app: app, bs: cp})
	}
	resp.Updated = resStats.updated
	resStats.mu.Unlock()

	for _, sn := range snaps {
		src, ok := getSource(sn.app)
		if !ok || src.Team != team {
			continue
		}
		appOptions = append(appOptions, sn.app)
		if only != "" && sn.app != only {
			continue
		}
		for t, b := range sn.bs {
			if t < startT {
				continue
			}
			bt := (t / bucketSecs) * bucketSecs
			a := buckets[bt]
			if a == nil {
				a = &agg{}
				buckets[bt] = a
			}
			a.cpuSum += b.CPUSum
			a.memSum += b.MemSum
			a.n += b.N
			resp.Restarts += b.Restarts
			resp.Samples += b.N
			if b.MemLimit > resp.MemLimit {
				resp.MemLimit = b.MemLimit
			}
			if b.N > 0 && t >= appLatestT[sn.app] {
				appLatestT[sn.app] = t
				appCur[sn.app] = &resourcesApp{Name: sn.app, CPU: b.CPUSum / float64(b.N), Mem: b.MemSum / int64(b.N)}
			}
		}
	}

	resp.Series = make([]resourcesSeries, 0, int((nowT-startT)/bucketSecs+1))
	for t := startT; t <= nowT; t += bucketSecs {
		s := resourcesSeries{T: t}
		if a := buckets[t]; a != nil && a.n > 0 {
			cpu := round2(a.cpuSum / float64(a.n))
			mem := a.memSum / int64(a.n)
			s.CPU, s.Mem = &cpu, &mem
			if cpu > resp.PeakCPU {
				resp.PeakCPU = cpu
			}
			if mem > resp.PeakMem {
				resp.PeakMem = mem
			}
		}
		resp.Series = append(resp.Series, s)
	}
	for i := len(resp.Series) - 1; i >= 0; i-- {
		if resp.Series[i].CPU != nil {
			resp.CurCPU = *resp.Series[i].CPU
			resp.CurMem = *resp.Series[i].Mem
			break
		}
	}

	resp.Apps = make([]resourcesApp, 0, len(appCur))
	for _, a := range appCur {
		a.CPU = round2(a.CPU)
		resp.Apps = append(resp.Apps, *a)
	}
	sort.Slice(resp.Apps, func(i, j int) bool { return resp.Apps[i].Mem > resp.Apps[j].Mem })
	if len(resp.Apps) > 8 {
		resp.Apps = resp.Apps[:8]
	}
	sort.Strings(appOptions)

	resp.AppOptions = appOptions
	resp.RangeMins = rangeMins
	resp.BucketSecs = int(bucketSecs)
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(resp)
}
