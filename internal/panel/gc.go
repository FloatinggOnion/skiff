package panel

import (
	"log"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/nuelScript/skiff/internal/docker"
	"github.com/nuelScript/skiff/internal/registry"
)

const (
	gcInterval      = 24 * time.Hour
	gcDiskPressure  = 0.85
	buildCacheAge   = 7 * 24 * time.Hour
	imageGraceAfter = time.Hour // a first deploy's image exists before its app is registered
)

type gcReport struct {
	Removed   []string `json:"removed"`
	Freed     int64    `json:"freed"`
	DiskUsed  int64    `json:"diskUsed"`
	DiskTotal int64    `json:"diskTotal"`
	Ran       int64    `json:"ran"`
}

var gcMu sync.Mutex

// staleAppImages picks the images of apps that no longer exist anywhere: no registry entry, no project, and no container built from them.
func staleAppImages(imgs []docker.AppImage, registered, projects, inUse map[string]bool, now time.Time) []string {
	var out []string
	for _, img := range imgs {
		if registered[img.App] || projects[img.App] || inUse[img.Ref] {
			continue
		}
		if now.Sub(img.Created) < imageGraceAfter {
			continue
		}
		out = append(out, img.Ref)
	}
	sort.Strings(out)
	return out
}

func (p *Panel) collectGarbage() gcReport {
	gcMu.Lock()
	defer gcMu.Unlock()
	before := diskUsage()
	rep := gcReport{Removed: []string{}, Ran: time.Now().Unix()}

	imgs, err := p.eng.AppImages()
	inUse, err2 := p.eng.ContainerImages()
	apps, err3 := registry.Load()
	if err == nil && err2 == nil && err3 == nil {
		registered := map[string]bool{}
		for name := range apps {
			registered[name] = true
		}
		projects := map[string]bool{}
		for _, s := range allSources() {
			projects[s.App] = true
		}
		for _, ref := range staleAppImages(imgs, registered, projects, inUse, time.Now()) {
			if p.eng.RemoveImage(ref) == nil {
				rep.Removed = append(rep.Removed, ref)
			}
		}
	}
	if err := p.eng.PruneDanglingImages(); err != nil {
		log.Printf("skiff: pruning dangling images: %v", err)
	}
	if err := p.eng.PruneBuildCache(buildCacheAge); err != nil {
		log.Printf("skiff: pruning build cache: %v", err)
	}

	after := diskUsage()
	rep.DiskUsed, rep.DiskTotal = after.Used, after.Total
	if before.Used > after.Used {
		rep.Freed = before.Used - after.Used
	}
	return rep
}

func (p *Panel) gcLoop() {
	time.Sleep(5 * time.Minute)
	var last time.Time
	for {
		guard("gcLoop", func() {
			recordDiskSample(time.Now())
			removeUnlistedBackups(time.Now())
			d := diskUsage()
			pressure := d.Total > 0 && float64(d.Used)/float64(d.Total) >= gcDiskPressure
			if time.Since(last) < gcInterval && !pressure {
				return
			}
			last = time.Now()
			rep := p.collectGarbage()
			if len(rep.Removed) > 0 || rep.Freed > 0 {
				log.Printf("skiff: reclaimed %d MB (%d unused app images)", rep.Freed>>20, len(rep.Removed))
			}
		})
		time.Sleep(time.Hour)
	}
}

func (p *Panel) handleServerGC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if !p.isInstanceAdmin(r) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	rep := p.collectGarbage()
	p.audit(r, "server.gc", "", "")
	writeJSON(w, rep)
}
