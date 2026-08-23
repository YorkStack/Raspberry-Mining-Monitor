package axeos

// Unit conversions from the upstream firmware's raw values to the normalised
// snapshot units. Each preserves nil, so a missing field stays missing rather
// than becoming a zero.

func ghsToThs(v *float64) *float64 {
	if v == nil {
		return nil
	}
	t := *v / 1000
	return &t
}

func mvToV(v *float64) *float64 {
	if v == nil {
		return nil
	}
	x := *v / 1000
	return &x
}

func maToA(v *float64) *float64 {
	if v == nil {
		return nil
	}
	x := *v / 1000
	return &x
}
