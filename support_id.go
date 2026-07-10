package berghain

import "bytes"

const supportIDLength = len("bh@00000000-0000-4000-8000-000000000000")

func ValidSupportID(id []byte) bool {
	if len(id) != supportIDLength || !bytes.Equal(id[:3], []byte("bh@")) {
		return false
	}

	for i, c := range id[3:] {
		switch i {
		case 8, 13, 18, 23:
			if c != '-' {
				return false
			}
		default:
			if !('0' <= c && c <= '9') && !('a' <= c && c <= 'f') && !('A' <= c && c <= 'F') {
				return false
			}
		}
	}

	return true
}
