package conversions

import (
	"fmt"

	"github.com/alecthomas/units"
)

func GbToBytes(gb int64) int64 {
	return gb * int64(units.GB)
}

func GibToBytes(gib int64) int64 {
	return gib * int64(units.GiB)
}

func GibToMib(gib int64) int64 {
	return gib * int64(units.KiB)
}

func MibToGiB(mib int64) int64 {
	return mib / int64(units.KiB)
}

func BytesToGb(bytes int64) int64 {
	return bytes / int64(units.GB)
}

func BytesToGib(bytes int64) int64 {
	return bytes / int64(units.GiB)
}

func MibToBytes(mib int64) int64 {
	return mib * int64(units.MiB)
}

func BytesToMib(bytes int64) int64 {
	return bytes / int64(units.MiB)
}

func GbToMib(gb int64) int64 {
	return BytesToMib(GbToBytes(gb))
}

const (
	_ = iota
	// KiB 1024 bytes
	KiB = 1 << (10 * iota)
	// MiB 1024 KiB
	MiB
	// GiB 1024 MiB
	GiB
	// TiB 1024 GiB
	TiB
	// PiB 1024 TiB
	PiB
)

const (
	KB = 1000
	MB = 1000 * KB
	GB = 1000 * MB
	TB = 1000 * GB
	PB = 1000 * TB
)

func BytesToString(b int64) string {
	if b >= PiB {
		return fmt.Sprintf("%.2f PiB", float64(b)/float64(PiB))
	}
	if b >= TiB {
		return fmt.Sprintf("%.2f TiB", float64(b)/float64(TiB))
	}
	if b >= GiB {
		return fmt.Sprintf("%.2f GiB", float64(b)/float64(GiB))
	}
	if b >= MiB {
		return fmt.Sprintf("%v MiB", b/MiB)
	}
	if b >= KiB {
		return fmt.Sprintf("%v KiB", b/KiB)
	}
	return fmt.Sprintf("%v bytes", b)
}
