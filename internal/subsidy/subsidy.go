// Package subsidy computes the Bitcoin block subsidy from a block height.
//
// The subsidy is fully determined by the height, so it is calculated locally
// rather than fetched from an API.
package subsidy

// HalvingInterval is the number of blocks between subsidy halvings.
const HalvingInterval = 210_000

// initialSats is the genesis-era subsidy, 50 BTC.
const initialSats = 5_000_000_000

// SatsPerBTC is the number of satoshis in one bitcoin.
const SatsPerBTC = 100_000_000

// Sats returns the block subsidy in satoshis at the given height.
// It returns 0 once the shift would exhaust the subsidy.
func Sats(height uint32) uint64 {
	halvings := height / HalvingInterval
	if halvings >= 64 {
		return 0
	}
	return initialSats >> halvings
}

// BTC returns the block subsidy in bitcoin at the given height.
func BTC(height uint32) float64 {
	return float64(Sats(height)) / SatsPerBTC
}

// NextHalvingHeight returns the height of the first halving strictly after the
// given height.
func NextHalvingHeight(height uint32) uint32 {
	return (height/HalvingInterval + 1) * HalvingInterval
}
