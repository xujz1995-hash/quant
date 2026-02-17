package market

import "math"

// EMA computes Exponential Moving Average for the given period.
// Returns a slice of the same length as prices; early values use SMA as seed.
func EMA(prices []float64, period int) []float64 {
	n := len(prices)
	if n == 0 || period <= 0 {
		return nil
	}
	out := make([]float64, n)
	k := 2.0 / float64(period+1)

	// seed with SMA of first `period` values
	seed := 0.0
	seedLen := period
	if seedLen > n {
		seedLen = n
	}
	for i := 0; i < seedLen; i++ {
		seed += prices[i]
	}
	out[0] = seed / float64(seedLen)

	for i := 1; i < n; i++ {
		out[i] = prices[i]*k + out[i-1]*(1-k)
	}
	return out
}

// MACD computes MACD line = EMA12 - EMA26. Returns a slice of the same length as prices.
func MACD(prices []float64) []float64 {
	ema12 := EMA(prices, 12)
	ema26 := EMA(prices, 26)
	n := len(prices)
	out := make([]float64, n)
	for i := 0; i < n; i++ {
		out[i] = ema12[i] - ema26[i]
	}
	return out
}

// RSI computes Relative Strength Index for the given period.
func RSI(prices []float64, period int) []float64 {
	n := len(prices)
	if n < 2 || period <= 0 {
		return make([]float64, n)
	}
	out := make([]float64, n)
	out[0] = 50 // neutral default

	avgGain := 0.0
	avgLoss := 0.0

	// initial averages
	initLen := period
	if initLen >= n {
		initLen = n - 1
	}
	for i := 1; i <= initLen; i++ {
		change := prices[i] - prices[i-1]
		if change > 0 {
			avgGain += change
		} else {
			avgLoss += math.Abs(change)
		}
	}
	avgGain /= float64(period)
	avgLoss /= float64(period)

	if avgLoss == 0 {
		out[initLen] = 100
	} else {
		rs := avgGain / avgLoss
		out[initLen] = 100 - 100/(1+rs)
	}

	for i := initLen + 1; i < n; i++ {
		change := prices[i] - prices[i-1]
		gain := 0.0
		loss := 0.0
		if change > 0 {
			gain = change
		} else {
			loss = math.Abs(change)
		}
		avgGain = (avgGain*float64(period-1) + gain) / float64(period)
		avgLoss = (avgLoss*float64(period-1) + loss) / float64(period)

		if avgLoss == 0 {
			out[i] = 100
		} else {
			rs := avgGain / avgLoss
			out[i] = 100 - 100/(1+rs)
		}
	}

	// fill early values
	for i := 1; i < initLen; i++ {
		out[i] = out[initLen]
	}
	return out
}

// ATR computes Average True Range from high, low, close arrays.
func ATR(highs, lows, closes []float64, period int) []float64 {
	n := len(closes)
	if n < 2 || period <= 0 {
		return make([]float64, n)
	}
	tr := make([]float64, n)
	tr[0] = highs[0] - lows[0]
	for i := 1; i < n; i++ {
		hl := highs[i] - lows[i]
		hc := math.Abs(highs[i] - closes[i-1])
		lc := math.Abs(lows[i] - closes[i-1])
		tr[i] = math.Max(hl, math.Max(hc, lc))
	}
	return EMA(tr, period)
}
