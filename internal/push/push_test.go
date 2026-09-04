package push

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang/snappy"

	"github.com/YorkStack/Raspberry-Mining-Monitor/internal/dashboard"
)

// TestMarshalWriteRequestExactBytes pins the hand-rolled protobuf encoding
// against a known-good byte sequence for a minimal WriteRequest: one series with
// a single __name__="x" label and a sample value 1 at timestamp 1000 ms. The
// expected bytes were derived by hand from the protobuf wire format, so this is
// an independent check of the encoder, not a round-trip against itself.
func TestMarshalWriteRequestExactBytes(t *testing.T) {
	got := marshalWriteRequest([]series{{
		labels: []label{{"__name__", "x"}},
		value:  1,
	}}, 1000)

	want := []byte{
		0x0A, 0x1D, // WriteRequest field 1 (timeseries), len 29
		0x0A, 0x0D, // TimeSeries field 1 (labels), len 13
		0x0A, 0x08, '_', '_', 'n', 'a', 'm', 'e', '_', '_', // Label field 1 "__name__"
		0x12, 0x01, 'x', // Label field 2 "x"
		0x12, 0x0C, // TimeSeries field 2 (samples), len 12
		0x09, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xF0, 0x3F, // Sample field 1 double 1.0
		0x10, 0xE8, 0x07, // Sample field 2 int64 1000
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("marshal mismatch\n got %x\nwant %x", got, want)
	}
}

func find(ss []series, name, miner string) (float64, bool) {
	for _, s := range ss {
		var gotName, gotMiner string
		for _, l := range s.labels {
			switch l.name {
			case "__name__":
				gotName = l.value
			case "miner":
				gotMiner = l.value
			}
		}
		if gotName == name && gotMiner == miner {
			return s.value, true
		}
	}
	return 0, false
}

func TestBuildSeriesCoversFleetMinersAndNetwork(t *testing.T) {
	p := 29.0
	temp := 46.0
	var acc uint64 = 6822
	v := dashboard.View{
		Miners: []dashboard.MinerView{{
			Name: "CocoMiner2", Online: true, HashrateTHs: 1.74,
			PowerW: &p, ASICTempC: &temp, SharesAccepted: &acc,
		}},
		Totals:  dashboard.TotalsView{HashrateTHs: 1.74, PowerW: 29, MinersOnline: 1, MinersTotal: 2},
		Network: dashboard.NetworkView{Difficulty: 1.2e14, NetworkHashrateHs: 8e20, Height: 900000, PriceEUR: 55000},
	}
	ss := buildSeries(v, "0.15.0")

	if got, ok := find(ss, "rmm_miner_hashrate_ths", "CocoMiner2"); !ok || got != 1.74 {
		t.Errorf("miner hashrate = %v ok=%v", got, ok)
	}
	if got, ok := find(ss, "rmm_miner_power_watts", "CocoMiner2"); !ok || got != 29 {
		t.Errorf("miner power = %v ok=%v", got, ok)
	}
	if got, ok := find(ss, "rmm_miner_online", "CocoMiner2"); !ok || got != 1 {
		t.Errorf("miner online = %v ok=%v", got, ok)
	}
	if got, ok := find(ss, "rmm_fleet_hashrate_ths", ""); !ok || got != 1.74 {
		t.Errorf("fleet hashrate = %v ok=%v", got, ok)
	}
	if got, ok := find(ss, "rmm_network_height", ""); !ok || got != 900000 {
		t.Errorf("network height = %v ok=%v", got, ok)
	}
	if _, ok := find(ss, "rmm_build_info", ""); !ok {
		t.Error("build_info missing")
	}
	// VRM temp was nil, so no series should be emitted for it.
	if _, ok := find(ss, "rmm_miner_vrm_temp_celsius", "CocoMiner2"); ok {
		t.Error("vrm temp series emitted despite nil value")
	}
	// Every series must carry __name__ and its labels must be sorted by name.
	for _, s := range ss {
		hasName := false
		for i, l := range s.labels {
			if l.name == "__name__" {
				hasName = true
			}
			if i > 0 && s.labels[i-1].name > l.name {
				t.Errorf("labels not sorted for %v", s.labels)
			}
		}
		if !hasName {
			t.Errorf("series without __name__: %v", s.labels)
		}
	}
}

func TestPushSendsSnappyProtobufWithAuth(t *testing.T) {
	var gotBody []byte
	var gotUser, gotPass string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if ce := r.Header.Get("Content-Encoding"); ce != "snappy" {
			t.Errorf("content-encoding = %q", ce)
		}
		if v := r.Header.Get("X-Prometheus-Remote-Write-Version"); v != "0.1.0" {
			t.Errorf("remote-write-version = %q", v)
		}
		gotUser, gotPass, _ = r.BasicAuth()
		raw, _ := io.ReadAll(r.Body)
		dec, err := snappy.Decode(nil, raw)
		if err != nil {
			t.Errorf("body not snappy: %v", err)
		}
		gotBody = dec
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := New(Config{URL: srv.URL, User: "3561754", Token: "secret", Timeout: 5 * time.Second, Version: "0.15.0"}, nil)
	v := dashboard.View{Miners: []dashboard.MinerView{{Name: "m1", Online: true, HashrateTHs: 1}}}
	if err := c.Push(context.Background(), v, time.Unix(1, 0)); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if gotUser != "3561754" || gotPass != "secret" {
		t.Errorf("basic auth = %q/%q", gotUser, gotPass)
	}
	if len(gotBody) == 0 {
		t.Error("empty decoded body")
	}
}

func TestPushReturnsErrorOnNon2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	defer srv.Close()
	c := New(Config{URL: srv.URL, User: "u", Token: "t", Timeout: 5 * time.Second}, nil)
	err := c.Push(context.Background(), dashboard.View{}, time.Unix(1, 0))
	if err == nil {
		t.Error("expected error on 500")
	}
}
