package httpencoding

import (
	"strconv"
	"strings"
)

// Best respects explicit q=0 refusals; ties prefer the first available codec.
func Best(header string, available ...string) string {
	qualities := map[string]float64{}
	for _, item := range strings.Split(header, ",") {
		parts := strings.Split(item, ";")
		name := strings.ToLower(strings.TrimSpace(parts[0]))
		q := 1.0
		for _, part := range parts[1:] {
			key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
			if ok && key == "q" {
				var err error
				q, err = strconv.ParseFloat(value, 64)
				if err != nil || q < 0 || q > 1 {
					q = 0
				}
			}
		}
		qualities[name] = q
	}
	best, quality := "", 0.0
	for _, name := range available {
		q, ok := qualities[name]
		if !ok {
			q = qualities["*"]
		}
		if q > quality {
			best, quality = name, q
		}
	}
	return best
}
