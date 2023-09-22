package berghain

import (
	"net/netip"
	"testing"
	"time"
)

func BenchmarkRequestIdentifier_ToCookie(b *testing.B) {
	bh := NewBerghain(generateSecret(b))

	bh.Levels = []*LevelConfig{
		{
			Duration: time.Minute,
			Type:     ValidationTypeNone,
		},
	}

	var ri = RequestIdentifier{
		SrcAddr: netip.MustParseAddr("1.2.3.4"),
		Host:    []byte("example.com"),
		Level:   1,
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			cb := AcquireCookieBuffer()
			if err := ri.ToCookie(bh, cb); err != nil {
				b.Fatal(err)
			}
			ReleaseCookieBuffer(cb)
		}
	})
}

func BenchmarkRequestIdentifier_IsValidCookie(b *testing.B) {
	bh := NewBerghain(generateSecret(b))

	bh.Levels = []*LevelConfig{
		{
			Duration: time.Minute,
			Type:     ValidationTypeNone,
		},
	}

	var ri = RequestIdentifier{
		SrcAddr: netip.MustParseAddr("1.2.3.4"),
		Host:    []byte("example.com"),
		Level:   1,
	}

	cb := AcquireCookieBuffer()
	defer ReleaseCookieBuffer(cb)
	if err := ri.ToCookie(bh, cb); err != nil {
		b.Fatal(err)
	}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			if err := bh.IsValidCookie(ri, cb.ReadBytes()); err != nil {
				b.Fatal(err)
			}
		}
	})
}
