package keymutex

// HashHandle for keymutex
type HashHandle func(key string) uint

// ELFHash Function
func ELFHash(key string) uint {
	h := uint(0)
	for i := 0; i < len(key); i++ {
		h = (h << 4) + uint(key[i])
		g := h & 0xF0000000
		if g != 0 {
			h ^= g >> 24
		}
		h &= ^g //~g
	}
	return h
}

// SDBMHash Function
func SDBMHash(key string) uint {
	hash := uint(0)
	for i := 0; i < len(key); i++ {
		// equivalent to: hash = 65599*hash + (*str++);
		hash = uint(key[i]) + (hash << 6) + (hash << 16) - hash
	}
	return hash
}

// RSHash Function
func RSHash(key string) uint {
	b := uint(378551)
	a := uint(63689)
	hash := uint(0)
	for i := 0; i < len(key); i++ {
		hash = hash*a + uint(key[i])
		a *= b
	}
	return hash
}

// JSHash Function
func JSHash(key string) uint {
	hash := uint(1315423911)
	for i := 0; i < len(key); i++ {
		hash ^= ((hash << 5) + uint(key[i]) + (hash >> 2))
	}
	return hash
}

// PJWHash P. J. Weinberger Hash Function
func PJWHash(key string) uint {
	BitsInUnignedInt := uint(4 * 8) //sizeof(unsigned int) * 8
	ThreeQuarters := uint((BitsInUnignedInt * 3) / 4)
	OneEighth := uint(BitsInUnignedInt / 8)
	HighBits := uint(0xFFFFFFFF) << (BitsInUnignedInt - OneEighth)
	hash := uint(0)
	test := uint(0)
	for i := 0; i < len(key); i++ {
		hash = (hash << OneEighth) + uint(key[i])
		if test = hash & HighBits; test != 0 {
			hash = ((hash ^ (test >> ThreeQuarters)) & (^HighBits))
		}
	}
	return hash
}

// BKDRHash Function
func BKDRHash(key string) uint {
	seed := uint(131) // 31 131 1313 13131 131313 etc..
	hash := uint(0)
	for i := 0; i < len(key); i++ {
		hash = hash*seed + uint(key[i])
	}
	return hash
}

// DJBHash Function
func DJBHash(key string) uint {
	hash := uint(5381)
	for i := 0; i < len(key); i++ {
		hash += (hash << 5) + uint(key[i])
	}
	return hash
}

// APHash Function
func APHash(key string) uint {
	hash := uint(0)
	for i := 0; i < len(key); i++ {
		if (i & 1) == 0 {
			hash ^= ((hash << 7) ^ uint(key[i]) ^ (hash >> 3))
		} else {
			hash ^= (^((hash << 11) ^ uint(key[i]) ^ (hash >> 5)))
		}
	}
	return hash
}
