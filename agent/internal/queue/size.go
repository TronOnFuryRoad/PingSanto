package queue

import (
	"fmt"
	"strconv"
	"strings"
)

var sizeUnitsOrdered = []struct {
	suffix     string
	multiplier int64
}{
	{"tib", 1024 * 1024 * 1024 * 1024},
	{"tb", 1000 * 1000 * 1000 * 1000},
	{"gib", 1024 * 1024 * 1024},
	{"gb", 1000 * 1000 * 1000},
	{"mib", 1024 * 1024},
	{"mb", 1000 * 1000},
	{"kib", 1024},
	{"kb", 1000},
	{"b", 1},
}

func ParseSize(value string, defaultBytes int64) (int64, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return defaultBytes, nil
	}
	lower := strings.ToLower(value)
	for _, unit := range sizeUnitsOrdered {
		if strings.HasSuffix(lower, unit.suffix) {
			numStr := strings.TrimSpace(value[:len(value)-len(unit.suffix)])
			num, err := strconv.ParseFloat(numStr, 64)
			if err != nil {
				return 0, fmt.Errorf("parse size %q: %w", value, err)
			}
			return int64(num * float64(unit.multiplier)), nil
		}
	}
	// fallback to plain integer bytes
	num, err := strconv.ParseInt(value, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("parse size %q: %w", value, err)
	}
	return num, nil
}
