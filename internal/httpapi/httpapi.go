// Package httpapi serves the dashboard document over REST and SSE, plus the
// embedded frontend.
//
// The API is read-only. Write methods are refused at the router, so there is
// no code path from the browser to a miner.
package httpapi

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/dashboard"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/history"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/minercfg"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/settings"
	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/state"
)

// contentSecurityPolicy forbids every external origin. Everything the page
// needs ships in the binary.
const contentSecurityPolicy = "default-src 'self'; script-src 'self'; style-src 'self'; " +
	"img-src 'self' data:; connect-src 'self'; font-src 'self'; " +
	"object-src 'none'; frame-ancestors 'none'; base-uri 'none'; form-action 'none'"

// Options configures the handler.
type Options struct {
	Store *state.Store
	Now   func() time.Time

	// Intervals carries the poll intervals and temperature thresholds used to
	// derive staleness and warning levels. The snapshot fields are ignored.
	Intervals dashboard.Input

	// Static is the embedded frontend.
	Static fs.FS

	Version string

	// Settings holds the operator-adjustable display thresholds. The settings
	// surface is reachable from loopback only, so the LAN dashboard stays
	// strictly read-only.
	Settings        *settings.Store
	SettingsEnabled bool

	// MinerNames is the configured fleet. A threshold may only be set for a
	// miner that actually exists.
	MinerNames []string

	// ScreensaverSeconds is passed through to the UI for burn-in protection.
	ScreensaverSeconds int

	// MinerCfg is the editable miner/provider config. When set, the admin API
	// can read and replace it, and knowsMiner consults it for the live set.
	MinerCfg *minercfg.Store
	// OnConfigChange is invoked after the miner/provider config is replaced, so
	// the collectors can be reloaded.
	OnConfigChange func()

	// History serves the rolling fleet-total record for the charts.
	History *history.Store
}

func (o Options) minerNames() []string {
	if o.MinerCfg != nil {
		return o.MinerCfg.Names()
	}
	return o.MinerNames
}

func (o Options) knowsMiner(name string) bool {
	for _, n := range o.minerNames() {
		if n == name {
			return true
		}
	}
	return false
}

func (o Options) now() time.Time {
	if o.Now != nil {
		return o.Now()
	}
	return time.Now()
}

// view builds the current dashboard document.
//
// Thresholds are read from the settings store on every build rather than
// captured at startup, so an adjustment made on the settings page shows up in
// the very next frame.
func (o Options) view() dashboard.View {
	in := o.Intervals
	in.Miners = o.Store.Miners()
	in.Pool = o.Store.Pool()
	in.Network = o.Store.Network()

	if o.Settings != nil {
		in.Thresholds = dashboard.Thresholds(o.Settings.Default())
		ov := o.Settings.Overrides()
		perMiner := make(map[string]dashboard.Thresholds, len(ov))
		for name, t := range ov {
			perMiner[name] = dashboard.Thresholds(t)
		}
		in.MinerThresholds = perMiner
		in.DisabledMiners = o.Settings.Disabled()
		in.MinerIcons = o.Settings.Icons()
		sv := o.Settings.ScreensaverCfg()
		in.ScreensaverMode = sv.Mode
		if sv.Mode == "off" {
			in.ScreensaverSeconds = 0
		} else {
			in.ScreensaverSeconds = sv.Minutes * 60
		}
	} else {
		in.ScreensaverSeconds = o.ScreensaverSeconds
	}

	return dashboard.Build(in, o.now())
}

// NewHandler wires the read-only routes.
func NewHandler(o Options) http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/api/v1/snapshot", getOnly(o.handleSnapshot))
	mux.Handle("/api/v1/stream", getOnly(o.handleStream))
	mux.Handle("/api/v1/history", getOnly(o.handleHistory))
	mux.Handle("/api/v1/version", getOnly(o.handleVersion))
	mux.Handle("/history", getOnly(o.servePage("history.html")))

	// Operator surfaces. Loopback only, which on the Pi means the kiosk itself.
	mux.Handle("/healthz", o.trustedOnly(getOnly(o.handleHealth)))
	mux.Handle("/settings", o.operatorOnly(getOnly(o.handleSettingsPage)))
	mux.Handle("/api/v1/settings", o.operatorOnly(getOnly(o.handleSettingsGet)))
	mux.Handle("/api/v1/settings/screensaver", o.operatorOnly(http.HandlerFunc(o.handleScreensaver)))
	mux.Handle("/api/v1/settings/", o.operatorOnly(http.HandlerFunc(o.handleSettingsMutate)))
	mux.Handle("/api/v1/miners", o.minerCfgOnly(http.HandlerFunc(o.handleMiners)))
	mux.Handle("/api/v1/providers", o.minerCfgOnly(http.HandlerFunc(o.handleProviders)))

	mux.Handle("/", getOnly(noStore(http.FileServer(http.FS(o.Static)).ServeHTTP)))

	return securityHeaders(mux)
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", contentSecurityPolicy)
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Frame-Options", "DENY")
		next.ServeHTTP(w, r)
	})
}

// noStore stops the browser caching the embedded frontend, so the kiosk always
// picks up a freshly deployed HTML/CSS/JS instead of a stale copy from its own
// on-disk cache after an update. The assets are tiny and served locally, so the
// re-fetch cost is nil.
func noStore(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		next(w, r)
	}
}

// getOnly refuses anything that could mutate state. The monitor has no write
// path at all, and the router is where that is enforced.
func getOnly(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		next(w, r)
	})
}

func (o Options) handleHistory(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	rng := r.URL.Query().Get("range")
	if rng == "" {
		rng = "1h"
	}
	if o.History == nil {
		_ = json.NewEncoder(w).Encode(map[string]any{"range": rng, "points": []history.Point{}})
		return
	}
	// A miner query returns that miner's per-series history; otherwise the fleet
	// totals, plus the list of miners that have history for the UI selector.
	if name := r.URL.Query().Get("miner"); name != "" {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"range": rng, "miner": name,
			"points": o.History.QueryMiner(name, rng), "miners": o.History.MinerNames(),
		})
		return
	}
	_ = json.NewEncoder(w).Encode(map[string]any{
		"range": rng, "points": o.History.Query(rng), "miners": o.History.MinerNames(),
	})
}

func (o Options) handleSnapshot(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(o.view())
}

func (o Options) handleStream(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream; charset=utf-8")
	h.Set("Cache-Control", "no-store")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	changes, cancel := o.Store.Subscribe()
	defer cancel()

	send := func() bool {
		payload, err := json.Marshal(o.view())
		if err != nil {
			return false
		}
		if _, err := fmt.Fprintf(w, "event: snapshot\ndata: %s\n\n", payload); err != nil {
			return false
		}
		flusher.Flush()
		return true
	}

	if !send() {
		return
	}

	// A heartbeat keeps intermediaries from closing an idle connection and
	// lets the browser notice a dead backend.
	heartbeat := time.NewTicker(20 * time.Second)
	defer heartbeat.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case _, open := <-changes:
			if !open {
				return
			}
			if !send() {
				return
			}
		case <-heartbeat.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

type healthSource struct {
	Name       string  `json:"name"`
	OK         bool    `json:"ok"`
	Stale      bool    `json:"stale"`
	HasData    bool    `json:"hasData"`
	AgeSeconds float64 `json:"ageSeconds"`
	Err        string  `json:"err,omitempty"`
}

// handleVersion reports the build version. It is public, unlike the operator
// surfaces: the whole fleet's UI shows the version in its footer.
func (o Options) handleVersion(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(map[string]string{"version": o.Version})
}

type healthResponse struct {
	Status  string         `json:"status"`
	Version string         `json:"version"`
	Now     time.Time      `json:"now"`
	Sources []healthSource `json:"sources"`
}

// handleHealth reports per-source freshness. It answers 200 even when
// degraded: the dashboard is still serving, and failing the healthcheck
// because a miner is unplugged would only get the service restarted in a loop.
func (o Options) handleHealth(w http.ResponseWriter, _ *http.Request) {
	now := o.now()
	resp := healthResponse{Status: "ok", Version: o.Version, Now: now}

	add := func(name string, ok, stale, hasData bool, age float64, errText string) {
		resp.Sources = append(resp.Sources, healthSource{
			Name: name, OK: ok, Stale: stale, HasData: hasData, AgeSeconds: age, Err: errText,
		})
		if !ok {
			resp.Status = "degraded"
		}
	}

	for _, m := range o.Store.Miners() {
		add("miner:"+m.Name, m.OK, m.Stale(now, o.Intervals.MinerInterval), m.HasData(), m.Age(now).Seconds(), m.Err)
	}

	p := o.Store.Pool()
	add("pool", p.OK, p.Stale(now, o.Intervals.PoolInterval), p.HasData(), p.Age(now).Seconds(), p.Err)

	n := o.Store.Network()
	add("network", n.OK, n.Stale(now, o.Intervals.NetworkInterval), n.HasData(), n.Age(now).Seconds(), n.Err)

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

// isTrusted reports whether the request came from this machine or the local
// network (loopback or an RFC1918 / unique-local / link-local address).
//
// The admin surface only changes what the dashboard shows, never a miner, so
// trusted-LAN access is acceptable and matches the Phase 0 posture. Public
// addresses are refused. X-Forwarded-For and X-Real-IP are deliberately
// ignored: honouring them would let any client claim a trusted source, so this
// service must not sit behind a reverse proxy with the admin surface enabled.
func isTrusted(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// trustedOnly hides a route from anything outside the local network. It answers
// 404 rather than 403 so the route's existence is not advertised.
func (o Options) trustedOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !isTrusted(r.RemoteAddr) {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// operatorOnly additionally requires the settings surface to be switched on
// and wired up.
func (o Options) operatorOnly(next http.Handler) http.Handler {
	return o.trustedOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !o.SettingsEnabled || o.Settings == nil {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

// servePage serves one embedded HTML file at a clean path.
func (o Options) servePage(name string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		f, err := o.Static.Open(name)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache, must-revalidate")
		buf := make([]byte, 64*1024)
		for {
			n, rerr := f.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
			}
			if rerr != nil {
				return
			}
		}
	}
}

func (o Options) handleSettingsPage(w http.ResponseWriter, r *http.Request) {
	f, err := o.Static.Open("settings.html")
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer f.Close()

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if rs, ok := f.(interface {
		Read([]byte) (int, error)
	}); ok {
		buf := make([]byte, 64*1024)
		for {
			n, err := rs.Read(buf)
			if n > 0 {
				if _, werr := w.Write(buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				return
			}
		}
	}
}

type settingsResponse struct {
	Default     settings.Thresholds            `json:"default"`
	Miners      []string                       `json:"miners"`
	Overrides   map[string]settings.Thresholds `json:"overrides"`
	Disabled    map[string]bool                `json:"disabled"`
	Icons       map[string]string              `json:"icons"`
	Screensaver settings.Screensaver           `json:"screensaver"`
}

func (o Options) handleSettingsGet(w http.ResponseWriter, _ *http.Request) {
	resp := settingsResponse{
		Default:     o.Settings.Default(),
		Miners:      o.minerNames(),
		Overrides:   o.Settings.Overrides(),
		Disabled:    o.Settings.Disabled(),
		Icons:       o.Settings.Icons(),
		Screensaver: o.Settings.ScreensaverCfg(),
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(resp)
}

// handleSettingsMutate serves PUT and DELETE on /api/v1/settings/{miner}.
//
// This writes a display threshold and nothing else. There is still no path
// from here to a miner.
func (o Options) handleSettingsMutate(w http.ResponseWriter, r *http.Request) {
	raw := strings.TrimPrefix(r.URL.Path, "/api/v1/settings/")

	// Sub-route: /{miner}/enabled toggles monitoring for that miner.
	if enc, ok := strings.CutSuffix(raw, "/enabled"); ok {
		o.handleToggleEnabled(w, r, enc)
		return
	}
	// Sub-route: /{miner}/icon sets the animated mark.
	if enc, ok := strings.CutSuffix(raw, "/icon"); ok {
		o.handleIcon(w, r, enc)
		return
	}

	name, err := url.PathUnescape(raw)
	if err != nil || name == "" {
		http.Error(w, "miner name required", http.StatusBadRequest)
		return
	}
	if !o.knowsMiner(name) {
		http.NotFound(w, r)
		return
	}

	switch r.Method {
	case http.MethodPut:
		var t settings.Thresholds
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&t); err != nil {
			http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
			return
		}
		if err := o.Settings.Set(name, t); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	case http.MethodDelete:
		o.Settings.Reset(name)
	default:
		w.Header().Set("Allow", "PUT, DELETE")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if err := o.Settings.Save(); err != nil {
		http.Error(w, "could not persist: "+err.Error(), http.StatusInternalServerError)
		return
	}
	o.handleSettingsGet(w, r)
}

// handleToggleEnabled switches monitoring for one miner on or off. Like every
// settings write, this changes only what the dashboard shows, never the miner.
func (o Options) handleToggleEnabled(w http.ResponseWriter, r *http.Request, encName string) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name, err := url.PathUnescape(encName)
	if err != nil || name == "" {
		http.Error(w, "miner name required", http.StatusBadRequest)
		return
	}
	if !o.knowsMiner(name) {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	o.Settings.SetEnabled(name, body.Enabled)
	if err := o.Settings.Save(); err != nil {
		http.Error(w, "could not persist: "+err.Error(), http.StatusInternalServerError)
		return
	}
	o.handleSettingsGet(w, r)
}

// minerCfgOnly gates the miner/provider config API: trusted network and a
// config store present.
func (o Options) minerCfgOnly(next http.Handler) http.Handler {
	return o.trustedOnly(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if o.MinerCfg == nil {
			http.NotFound(w, r)
			return
		}
		next.ServeHTTP(w, r)
	}))
}

type apiMiner struct {
	Name            string `json:"name"`
	Type            string `json:"type"`
	Host            string `json:"host"`
	PayoutAddress   string `json:"payoutAddress"`
	IntervalSeconds int    `json:"intervalSeconds"`
}

type minersResponse struct {
	Miners    []apiMiner         `json:"miners"`
	Providers minercfg.Providers `json:"providers"`
}

func (o Options) writeMiners(w http.ResponseWriter) {
	specs := o.MinerCfg.Miners()
	out := make([]apiMiner, 0, len(specs))
	for _, m := range specs {
		out = append(out, apiMiner{
			Name: m.Name, Type: m.Type, Host: m.Host, PayoutAddress: m.PayoutAddress,
			IntervalSeconds: int(m.Interval / time.Second),
		})
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_ = json.NewEncoder(w).Encode(minersResponse{Miners: out, Providers: o.MinerCfg.Providers()})
}

// handleMiners serves GET (list) and PUT (replace) of the miner list.
func (o Options) handleMiners(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		o.writeMiners(w)
	case http.MethodPut:
		var body struct {
			Miners []apiMiner `json:"miners"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
		dec.DisallowUnknownFields()
		if err := dec.Decode(&body); err != nil {
			http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
			return
		}
		// Preserve fields the admin UI never receives, keyed by miner name, so
		// editing a miner does not wipe its monitoring token or pool override.
		// The token in particular is never sent to the browser (see apiMiner).
		existing := make(map[string]minercfg.Spec)
		for _, m := range o.MinerCfg.Miners() {
			existing[m.Name] = m
		}
		specs := make([]minercfg.Spec, 0, len(body.Miners))
		for _, m := range body.Miners {
			spec := minercfg.Spec{
				Name: m.Name, Type: m.Type, Host: m.Host, PayoutAddress: m.PayoutAddress,
				Interval: time.Duration(m.IntervalSeconds) * time.Second,
			}
			if prev, ok := existing[m.Name]; ok {
				spec.Token = prev.Token
				spec.PoolProvider = prev.PoolProvider
				// Nominal/demo figures are not part of the admin form either, so
				// preserve them rather than zeroing a demo miner's hashrate.
				spec.NominalTHs = prev.NominalTHs
				spec.NominalW = prev.NominalW
				spec.NominalTempC = prev.NominalTempC
				spec.Model = prev.Model
				spec.Fans = prev.Fans
			}
			specs = append(specs, spec)
		}
		if err := o.MinerCfg.Replace(specs); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if o.OnConfigChange != nil {
			o.OnConfigChange()
		}
		o.writeMiners(w)
	default:
		w.Header().Set("Allow", "GET, PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleProviders serves PUT of the provider URLs.
func (o Options) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var p minercfg.Providers
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&p); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := o.MinerCfg.SetProviders(p); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if o.OnConfigChange != nil {
		o.OnConfigChange()
	}
	o.writeMiners(w)
}

// knownIcons is the set of animated marks the UI offers.
var knownIcons = map[string]bool{
	"i-chip-matrix": true, "i-reactor-core": true, "i-mac-laptop": true,
	"i-quantum-cube": true, "i-spectrum-bars": true, "i-hex-shield": true,
	"i-pulsar-beacon": true, "i-mining-drill": true,
}

// handleIcon sets a miner's animated mark. An empty id clears the override.
func (o Options) handleIcon(w http.ResponseWriter, r *http.Request, encName string) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	name, err := url.PathUnescape(encName)
	// "__total__" is the reserved key for the fleet-total tile's mark.
	if err != nil || name == "" || (name != "__total__" && !o.knowsMiner(name)) {
		http.NotFound(w, r)
		return
	}
	var body struct {
		Icon string `json:"icon"`
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if body.Icon != "" && !knownIcons[body.Icon] {
		http.Error(w, "unknown icon", http.StatusBadRequest)
		return
	}
	o.Settings.SetIcon(name, body.Icon)
	if err := o.Settings.Save(); err != nil {
		http.Error(w, "could not persist: "+err.Error(), http.StatusInternalServerError)
		return
	}
	o.handleSettingsGet(w, r)
}

// handleScreensaver sets the burn-in saver mode and timeout.
func (o Options) handleScreensaver(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPut {
		w.Header().Set("Allow", "PUT")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var cfg settings.Screensaver
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1024))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cfg); err != nil {
		http.Error(w, "invalid body: "+err.Error(), http.StatusBadRequest)
		return
	}
	if err := o.Settings.SetScreensaver(cfg); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := o.Settings.Save(); err != nil {
		http.Error(w, "could not persist: "+err.Error(), http.StatusInternalServerError)
		return
	}
	o.handleSettingsGet(w, r)
}
