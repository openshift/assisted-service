package network

import (
	"math"
	"net"
)

const (
	macAddressBytes  = 6
	apiVipPrefix     = "api"
	ingressVipPrefix = "ingress"
)

var macPrefixQumranet = [...]byte{0x00, 0x1A, 0x4A}

func pearsonHash(origin []byte, outputLength int) (hash []uint8) {
	// table for Pearson hashing from RFC 3074.
	lookupTable := [...]uint8{
		251, 175, 119, 215, 81, 14, 79, 191, 103, 49, 181, 143, 186, 157, 0,
		232, 31, 32, 55, 60, 152, 58, 17, 237, 174, 70, 160, 144, 220, 90, 57,
		223, 59, 3, 18, 140, 111, 166, 203, 196, 134, 243, 124, 95, 222, 179,
		197, 65, 180, 48, 36, 15, 107, 46, 233, 130, 165, 30, 123, 161, 209, 23,
		97, 16, 40, 91, 219, 61, 100, 10, 210, 109, 250, 127, 22, 138, 29, 108,
		244, 67, 207, 9, 178, 204, 74, 98, 126, 249, 167, 116, 34, 77, 193,
		200, 121, 5, 20, 113, 71, 35, 128, 13, 182, 94, 25, 226, 227, 199, 75,
		27, 41, 245, 230, 224, 43, 225, 177, 26, 155, 150, 212, 142, 218, 115,
		241, 73, 88, 105, 39, 114, 62, 255, 192, 201, 145, 214, 168, 158, 221,
		148, 154, 122, 12, 84, 82, 163, 44, 139, 228, 236, 205, 242, 217, 11,
		187, 146, 159, 64, 86, 239, 195, 42, 106, 198, 118, 112, 184, 172, 87,
		2, 173, 117, 176, 229, 247, 253, 137, 185, 99, 164, 102, 147, 45, 66,
		231, 52, 141, 211, 194, 206, 246, 238, 56, 110, 78, 248, 63, 240, 189,
		93, 92, 51, 53, 183, 19, 171, 72, 50, 33, 104, 101, 69, 8, 252, 83, 120,
		76, 135, 85, 54, 202, 125, 188, 213, 96, 235, 136, 208, 162, 129, 190,
		132, 156, 38, 47, 1, 7, 254, 24, 4, 216, 131, 89, 21, 28, 133, 37, 153,
		149, 80, 170, 68, 6, 169, 234, 151,
	}

	h := uint8(0)

	for i := 0; i < outputLength; i++ {
		// Change the first byte
		h = lookupTable[(int(origin[0])+i)%256]

		for _, c := range origin {
			h = lookupTable[h^c]
		}

		hash = append(hash, h)
	}

	return
}

func generateMac(prefix []byte, key string) (mac net.HardwareAddr) {
	mac = append(net.HardwareAddr{}, prefix...)

	hash := pearsonHash([]byte(key), int(math.Max(0, float64(macAddressBytes-len(prefix)))))
	mac = append(mac, hash...)

	return
}

func generateVipMAC(clusterID, vipName string) string {
	key := clusterID + "-" + vipName
	return generateMac(macPrefixQumranet[:], key).String()
}

func GenerateAPIVipMAC(clusterID string) string {
	return generateVipMAC(clusterID, apiVipPrefix)
}

func GenerateIngressVipMAC(clusterID string) string {
	return generateVipMAC(clusterID, ingressVipPrefix)
}
