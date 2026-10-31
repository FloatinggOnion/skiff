package panel

import (
	"encoding/json"
	"math"
	"net/http"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"
)

const costWindow = 24 * time.Hour

type costItem struct {
	Key      string  `json:"key"`
	Kind     string  `json:"kind"` // app | database | storage
	Name     string  `json:"name"`
	Team     string  `json:"team,omitempty"`
	CPU      float64 `json:"cpu"` // average % of one core
	Mem      int64   `json:"mem"` // average bytes
	Share    float64 `json:"share"`
	Monthly  float64 `json:"monthly"`
	EgressGB float64 `json:"egressGb"` // per month, from the last 7 days
}

type costReport struct {
	Price      float64    `json:"price"`
	Currency   string     `json:"currency"`
	Cores      int        `json:"cores"`
	MemTotal   int64      `json:"memTotal"`
	MemUsed    int64      `json:"memUsed"`
	Items      []costItem `json:"items"`
	System     float64    `json:"system"`   // OS, Docker and Skiff itself
	Idle       float64    `json:"idle"`     // capacity nothing is using
	Headroom   int        `json:"headroom"` // more apps of the median size that fit in free memory
	MedianMem  int64      `json:"medianMem"`
	DiskUsed   int64      `json:"diskUsed"`
	DiskTotal  int64      `json:"diskTotal"`
	DiskPerDay int64      `json:"diskPerDay"`
	DaysToFull int        `json:"daysToFull"` // -1 = not enough history or not growing
	Samples    int        `json:"samples"`
}

func getSetting(key string) string {
	var v string
	_ = sqlDB.QueryRow(`SELECT value FROM instance_settings WHERE key=?`, key).Scan(&v)
	return v
}

func setSetting(key, value string) error {
	_, err := sqlDB.Exec(`INSERT INTO instance_settings(key,value) VALUES(?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value`, key, value)
	return err
}

// usage averages each sampled container's CPU and memory over the window.
func usage(window time.Duration) (map[string]costItem, int) {
	cutoff := time.Now().Add(-window).Unix()
	out := map[string]costItem{}
	samples := 0
	resStats.mu.Lock()
	defer resStats.mu.Unlock()
	for key, buckets := range resStats.apps {
		var cpu float64
		var mem int64
		n := 0
		for t, b := range buckets {
			if t < cutoff || b.N == 0 {
				continue
			}
			cpu += b.CPUSum
			mem += b.MemSum
			n += b.N
		}
		if n == 0 {
			continue
		}
		samples += n
		out[key] = costItem{Key: key, CPU: cpu / float64(n), Mem: mem / int64(n)}
	}
	return out, samples
}

// share is the fraction of the box an item holds: half its slice of memory, half its slice of CPU capacity.
func share(cpu float64, mem int64, cores int, memTotal int64) float64 {
	if cores < 1 || memTotal <= 0 {
		return 0
	}
	s := 0.5*float64(mem)/float64(memTotal) + 0.5*cpu/(100*float64(cores))
	return math.Min(1, math.Max(0, s))
}

func egressByApp(days int) map[string]int64 {
	out := map[string]int64{}
	rows, err := sqlDB.Query(`SELECT app, SUM(bytes) FROM requests WHERE t >= ? GROUP BY app`,
		time.Now().AddDate(0, 0, -days).UnixMilli())
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var app string
		var b int64
		if rows.Scan(&app, &b) == nil {
			out[app] = b
		}
	}
	return out
}

func (p *Panel) buildCost() costReport {
	price, _ := strconv.ParseFloat(getSetting("server_cost"), 64)
	currency := getSetting("server_currency")
	if currency == "" {
		currency = "EUR"
	}
	mem := procMem()
	disk := diskUsage()
	rep := costReport{
		Price: price, Currency: currency, Cores: runtime.NumCPU(),
		MemTotal: mem.Total, MemUsed: mem.Used, Items: []costItem{},
		DiskUsed: disk.Used, DiskTotal: disk.Total, DaysToFull: -1,
	}
	used, samples := usage(costWindow)
	rep.Samples = samples

	names := map[string][2]string{} // key → kind, name
	teams := map[string]string{}
	for _, s := range allSources() {
		names[s.App] = [2]string{"app", s.App}
		teams[s.App] = s.Team
	}
	for _, d := range allDatabases() {
		names["db:"+d.ID] = [2]string{"database", d.Name}
		teams["db:"+d.ID] = d.Team
	}
	for _, b := range allBuckets() {
		names["bucket:"+b.ID] = [2]string{"storage", b.Name}
		teams["bucket:"+b.ID] = b.Team
	}
	egress := egressByApp(7)

	var attributed float64
	var attributedMem int64
	var appMems []int64
	for key, u := range used {
		meta, ok := names[key]
		if !ok {
			continue
		}
		u.Kind, u.Name, u.Team = meta[0], meta[1], teams[key]
		u.CPU = round2(u.CPU)
		u.Share = share(u.CPU, u.Mem, rep.Cores, rep.MemTotal)
		u.Monthly = math.Round(price*u.Share*100) / 100
		if b := egress[key]; b > 0 {
			u.EgressGB = math.Round(float64(b)/7*30/1e9*100) / 100
		}
		attributed += u.Share
		attributedMem += u.Mem
		if u.Kind == "app" {
			appMems = append(appMems, u.Mem)
		}
		rep.Items = append(rep.Items, u)
	}
	sort.Slice(rep.Items, func(i, j int) bool {
		if rep.Items[i].Share != rep.Items[j].Share {
			return rep.Items[i].Share > rep.Items[j].Share
		}
		return rep.Items[i].Name < rep.Items[j].Name
	})

	if rep.MemTotal > 0 {
		systemMem := rep.MemUsed - attributedMem
		if systemMem < 0 {
			systemMem = 0
		}
		systemShare := math.Min(1-attributed, 0.5*float64(systemMem)/float64(rep.MemTotal))
		if systemShare < 0 {
			systemShare = 0
		}
		rep.System = math.Round(price*systemShare*100) / 100
		rep.Idle = math.Round(price*math.Max(0, 1-attributed-systemShare)*100) / 100
	}

	if len(appMems) > 0 {
		sort.Slice(appMems, func(i, j int) bool { return appMems[i] < appMems[j] })
		rep.MedianMem = appMems[len(appMems)/2]
		if free := rep.MemTotal - rep.MemUsed; rep.MedianMem > 0 && free > 0 {
			rep.Headroom = int(free / rep.MedianMem)
		}
	}
	rep.DiskPerDay, rep.DaysToFull = diskForecast(rep.DiskTotal - rep.DiskUsed)
	return rep
}

func recordDiskSample(now time.Time) {
	d := diskUsage()
	if d.Total == 0 {
		return
	}
	_, _ = sqlDB.Exec(`INSERT INTO disk_samples(t,used,total) VALUES(?,?,?)`, now.Unix(), d.Used, d.Total)
	_, _ = sqlDB.Exec(`DELETE FROM disk_samples WHERE t < ?`, now.AddDate(0, 0, -30).Unix())
}

// diskForecast fits a line through the last week of disk samples and says how many days of growth the free space covers.
func diskForecast(free int64) (perDay int64, days int) {
	rows, err := sqlDB.Query(`SELECT t, used FROM disk_samples WHERE t >= ? ORDER BY t`, time.Now().AddDate(0, 0, -7).Unix())
	if err != nil {
		return 0, -1
	}
	defer rows.Close()
	var xs, ys []float64
	for rows.Next() {
		var t, used int64
		if rows.Scan(&t, &used) == nil {
			xs = append(xs, float64(t))
			ys = append(ys, float64(used))
		}
	}
	if len(xs) < 2 || xs[len(xs)-1]-xs[0] < 24*3600 {
		return 0, -1
	}
	slope := slopeOf(xs, ys) * 86400
	perDay = int64(slope)
	if slope <= 0 || free <= 0 {
		return perDay, -1
	}
	return perDay, int(float64(free) / slope)
}

func slopeOf(xs, ys []float64) float64 {
	n := float64(len(xs))
	var sx, sy, sxy, sxx float64
	for i := range xs {
		sx += xs[i]
		sy += ys[i]
		sxy += xs[i] * ys[i]
		sxx += xs[i] * xs[i]
	}
	den := n*sxx - sx*sx
	if den == 0 {
		return 0
	}
	return (n*sxy - sx*sy) / den
}

func (p *Panel) handleCost(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		rep := p.buildCost()
		if !p.isInstanceAdmin(r) {
			// Members see their own team's items, and only the shape of the box.
			team := p.teamID(r)
			mine := []costItem{}
			for _, it := range rep.Items {
				if it.Team == team {
					mine = append(mine, it)
				}
			}
			rep = costReport{Price: rep.Price, Currency: rep.Currency, Items: mine, Samples: rep.Samples, DaysToFull: -1}
		}
		writeJSON(w, rep)
	case http.MethodPut:
		if !p.isInstanceAdmin(r) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		var body struct {
			Price    float64 `json:"price"`
			Currency string  `json:"currency"`
		}
		if json.NewDecoder(r.Body).Decode(&body) != nil || body.Price < 0 || body.Price > 1e6 {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		cur := strings.ToUpper(strings.TrimSpace(body.Currency))
		if len(cur) != 3 {
			cur = "EUR"
		}
		if setSetting("server_cost", strconv.FormatFloat(body.Price, 'f', 2, 64)) != nil || setSetting("server_currency", cur) != nil {
			http.Error(w, "couldn't save", http.StatusInternalServerError)
			return
		}
		p.audit(r, "server.cost", "", strconv.FormatFloat(body.Price, 'f', 2, 64)+" "+cur)
		w.WriteHeader(http.StatusNoContent)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}
