package berghain

import "hash"

// zeroHasher provides a wrapper to create zero allocation hashes.
type zeroHasher struct {
	h   hash.Hash
	buf []byte
}

func NewZeroHasher(h hash.Hash) hash.Hash {
	return &zeroHasher{
		h:   h,
		buf: make([]byte, 0, h.Size()),
	}
}

func (zh *zeroHasher) Sum(b []byte) []byte {
	if b != nil {
		panic("zeroHasher does not support any parameter for Sum()")
	}
	if len(zh.buf) != 0 {
		panic("invalid buffer state")
	}
	return zh.h.Sum(zh.buf)
}

func (zh *zeroHasher) Size() int {
	return zh.h.Size()
}

func (zh *zeroHasher) BlockSize() int {
	return zh.h.BlockSize()
}

func (zh *zeroHasher) Write(p []byte) (int, error) {
	return zh.h.Write(p)
}

func (zh *zeroHasher) Reset() {
	zh.buf = zh.buf[:0]
	zh.h.Reset()
}
