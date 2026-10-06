package backend

import (
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/resource"
)

// HumanMilli formats CPU millicores: 250 -> 250m, 1500 -> 1.5 cores.
func HumanMilli(m int64) string {
	if m < 1000 {
		return strconv.FormatInt(m, 10) + "m"
	}
	c := trimDec(float64(m)/1000, 2)
	if c == "1" {
		return "1 core"
	}
	return c + " cores"
}

// HumanBytes formats a size with binary units: 1536 MiB -> 1.5 GiB.
func HumanBytes(b int64) string {
	units := []string{"B", "KiB", "MiB", "GiB", "TiB", "PiB"}
	f, i := float64(b), 0
	for f >= 1024 && i < len(units)-1 {
		f /= 1024
		i++
	}
	if i == 0 {
		return strconv.FormatInt(b, 10) + " B"
	}
	return trimDec(f, 1) + " " + units[i]
}

// HumanQuantity formats a resource quantity by kind: cores for CPU, binary
// units for memory and storage, the canonical string otherwise.
func HumanQuantity(r v1.ResourceName, q resource.Quantity) string {
	switch r {
	case v1.ResourceCPU:
		return HumanMilli(q.MilliValue())
	case v1.ResourceMemory, v1.ResourceEphemeralStorage, v1.ResourceStorage:
		return HumanBytes(q.Value())
	}
	return q.String()
}

func trimDec(f float64, prec int) string {
	s := strconv.FormatFloat(f, 'f', prec, 64)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}
