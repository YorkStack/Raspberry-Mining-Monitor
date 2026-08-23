package httpapi

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/probability"
)

// probWindow is the probability and 1-in-N odds for one time window.
type probWindow struct {
	Probability float64 `json:"probability"`
	OddsAgainst float64 `json:"oddsAgainst"`
}

// probabilityResponse answers GET /api/v1/probability. Without ?ths it reports
// the current combined hashrate; with ?ths=<TH/s> it reports a hypothetical
// hashrate against the same live network difficulty (the "what if" calculator).
type probabilityResponse struct {
	Ths                 float64    `json:"ths"`
	UsingCurrent        bool       `json:"usingCurrent"`
	CombinedHashrateThs float64    `json:"combinedHashrateThs"`
	Difficulty          float64    `json:"difficulty"`
	NetworkHashrateHs   float64    `json:"networkHashrateHs"`
	AsOf                time.Time  `json:"asOf"`
	NextBlock           probWindow `json:"nextBlock"`
	Day                 probWindow `json:"day"`
	Week                probWindow `json:"week"`
	Month               probWindow `json:"month"`
	Year                probWindow `json:"year"`
	MeanSeconds         float64    `json:"meanSeconds"`
	MeanYears           float64    `json:"meanYears"`
}

func probWindowFor(hashrateHs, difficulty, seconds float64) probWindow {
	p := probability.AtLeastOne(hashrateHs, difficulty, seconds)
	return probWindow{Probability: p, OddsAgainst: probability.OddsAgainst(p)}
}

// handleProbability computes solo-block odds for the current or a hypothetical
// hashrate, keeping all the maths in the probability module rather than the UI.
func (o Options) handleProbability(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")

	n := o.Store.Network()
	combined := o.view().Totals.HashrateTHs

	ths := combined
	usingCurrent := true
	if q := r.URL.Query().Get("ths"); q != "" {
		if val, err := strconv.ParseFloat(q, 64); err == nil && val >= 0 {
			ths = val
			usingCurrent = false
		}
	}

	resp := probabilityResponse{
		Ths:                 ths,
		UsingCurrent:        usingCurrent,
		CombinedHashrateThs: combined,
		Difficulty:          n.Difficulty,
		NetworkHashrateHs:   n.NetworkHashrateHs,
		AsOf:                n.FetchedAt,
	}

	if n.Difficulty > 0 && ths > 0 {
		hs := ths * 1e12
		resp.NextBlock = probWindowFor(hs, n.Difficulty, probability.NextBlock)
		resp.Day = probWindowFor(hs, n.Difficulty, probability.Day)
		resp.Week = probWindowFor(hs, n.Difficulty, probability.Week)
		resp.Month = probWindowFor(hs, n.Difficulty, probability.Month)
		resp.Year = probWindowFor(hs, n.Difficulty, probability.Year)
		if mean, ok := probability.MeanTimeToBlockSeconds(hs, n.Difficulty); ok {
			resp.MeanSeconds = mean
			resp.MeanYears = mean / probability.Year
		}
	}

	_ = json.NewEncoder(w).Encode(resp)
}
