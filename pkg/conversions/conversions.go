package conversions

import "github.com/alecthomas/units"

func GbToBytes(gb int64) int64 {
	return gb * int64(units.GB)
}

func GibToBytes(gib int64) int64 {
	return gib * int64(units.GiB)
}

func BytesToGiB(bytes int64) int64 {
	return bytes / int64(units.GiB)
}

func MibToBytes(mib int64) int64 {
	return mib * int64(units.MiB)
}

func BytesToMib(bytes int64) int64 {
	return bytes / int64(units.MiB)
}
