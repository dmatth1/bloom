package bloom

import (
	"encoding/binary"
	"math/rand"
	"sync"
	"testing"
)

func TestSBBFBasic(t *testing.T) {
	f := NewSBBF(64)
	n1 := []byte("Bess")
	n2 := []byte("Jane")
	n3 := []byte("Emma")
	f.Add(n1)
	n3a := f.TestAndAdd(n3)
	n1b := f.Test(n1)
	n2b := f.Test(n2)
	n3b := f.Test(n3)
	if !n1b {
		t.Errorf("%v should be in.", n1)
	}
	if n2b {
		t.Errorf("%v should not be in.", n2)
	}
	if n3a {
		t.Errorf("%v should not be in the first time we look.", n3)
	}
	if !n3b {
		t.Errorf("%v should be in the second time we look.", n3)
	}
}

func TestSBBFString(t *testing.T) {
	f := NewSBBF(64)
	n1 := "Love"
	n2 := "is"
	n3 := "in"
	n4 := "bloom"
	f.AddString(n1)
	n3a := f.TestAndAddString(n3)
	n1b := f.TestString(n1)
	n2b := f.TestString(n2)
	n3b := f.TestString(n3)
	if !n1b {
		t.Errorf("%v should be in.", n1)
	}
	if n2b {
		t.Errorf("%v should not be in.", n2)
	}
	if n3a {
		t.Errorf("%v should not be in the first time we look.", n3)
	}
	if !n3b {
		t.Errorf("%v should be in the second time we look.", n3)
	}
	if !f.TestString(n3) {
		t.Errorf("%v should still be in after re-test.", n3)
	}
	if f.TestString(n4) {
		t.Errorf("%v should not be in.", n4)
	}
}

func TestSBBFRequiresPowerOfTwo(t *testing.T) {
	for _, n := range []uint64{0, 3, 5, 6, 7, 9, 100} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("NewSBBF(%d) should panic", n)
				}
			}()
			_ = NewSBBF(n)
		}()
	}
	for _, n := range []uint64{1, 2, 4, 8, 64, 1024} {
		_ = NewSBBF(n) // must not panic
	}
}

func TestSBBFNoFalseNegatives(t *testing.T) {
	const n = 100_000
	f := NewSBBFWithEstimates(n, 0.01)
	keys := make([][]byte, n)
	for i := 0; i < n; i++ {
		k := make([]byte, 8)
		binary.LittleEndian.PutUint64(k, uint64(i))
		keys[i] = k
		f.Add(k)
	}
	for _, k := range keys {
		if !f.Test(k) {
			t.Fatalf("false negative for key %x", k)
		}
	}
}

func TestSBBFAddHashAgreesWithAdd(t *testing.T) {
	// Test/Add via []byte and TestHash/AddHash via the same baseHashes[0]
	// must produce identical filter contents and identical Test results.
	const n = 5000
	fA := NewSBBF(64)
	fB := NewSBBF(64)
	for i := 0; i < n; i++ {
		k := make([]byte, 8)
		binary.LittleEndian.PutUint64(k, uint64(i))
		h := baseHashes(k)[0]
		fA.Add(k)
		fB.AddHash(h)
	}
	if !fA.Equal(fB) {
		t.Fatal("Add and AddHash produced different filter contents")
	}
	// Probe both via Test and TestHash; results must agree.
	rng := rand.New(rand.NewSource(0xC0FFEE))
	for i := 0; i < 10_000; i++ {
		k := make([]byte, 8)
		binary.LittleEndian.PutUint64(k, rng.Uint64())
		h := baseHashes(k)[0]
		if fA.Test(k) != fB.TestHash(h) {
			t.Fatalf("Test and TestHash disagree on key %x", k)
		}
	}
}

func TestSBBFClearAll(t *testing.T) {
	f := NewSBBF(64)
	for i := 0; i < 1000; i++ {
		k := make([]byte, 8)
		binary.LittleEndian.PutUint64(k, uint64(i))
		f.Add(k)
	}
	f.ClearAll()
	for i := 0; i < 1000; i++ {
		k := make([]byte, 8)
		binary.LittleEndian.PutUint64(k, uint64(i))
		if f.Test(k) {
			t.Fatalf("ClearAll left a positive Test for key %x", k)
		}
	}
}

func TestSBBFEqual(t *testing.T) {
	f := NewSBBF(64)
	g := NewSBBF(64)
	if !f.Equal(g) {
		t.Error("two empty SBBFs of the same shape should be Equal")
	}
	f.Add([]byte("hello"))
	if f.Equal(g) {
		t.Error("SBBFs with different contents should not be Equal")
	}
	g.Add([]byte("hello"))
	if !f.Equal(g) {
		t.Error("SBBFs with the same contents should be Equal")
	}
	h := NewSBBF(128)
	if f.Equal(h) {
		t.Error("SBBFs with different block counts should not be Equal")
	}
}

func TestSBBFCapAndNumBlocks(t *testing.T) {
	for _, b := range []uint64{1, 2, 4, 16, 1024} {
		f := NewSBBF(b)
		if f.NumBlocks() != b {
			t.Errorf("NumBlocks: got %d, want %d", f.NumBlocks(), b)
		}
		if f.Cap() != b*256 {
			t.Errorf("Cap: got %d, want %d", f.Cap(), b*256)
		}
	}
}

func TestSBBFK(t *testing.T) {
	f := NewSBBF(64)
	if f.K() != 8 {
		t.Errorf("K: got %d, want 8", f.K())
	}
}

func TestSBBFTestOrAdd(t *testing.T) {
	// First call returns false (not present); second call returns true
	// (present) and must not modify the filter further. We verify the
	// "filter is unchanged" contract by snapshotting the block contents.
	f := NewSBBF(64)
	key := []byte("Love is in bloom")

	if f.TestOrAdd(key) {
		t.Fatal("first TestOrAdd should report not-present")
	}
	snapshot := make([]uint32, len(f.blocks))
	copy(snapshot, f.blocks)

	if !f.TestOrAdd(key) {
		t.Error("second TestOrAdd should report present")
	}
	for i, b := range f.blocks {
		if b != snapshot[i] {
			t.Fatalf("TestOrAdd modified the filter on a present key (block %d)", i)
		}
	}

	if !f.TestOrAddString("Love is in bloom") {
		t.Error("TestOrAddString should report present for the same key")
	}
}

func TestSBBFNextPow2(t *testing.T) {
	cases := []struct{ in, want uint64 }{
		{0, 1}, {1, 1}, {2, 2}, {3, 4}, {4, 4}, {5, 8},
		{7, 8}, {8, 8}, {9, 16}, {1024, 1024}, {1025, 2048},
	}
	for _, c := range cases {
		if got := nextPow2(c.in); got != c.want {
			t.Errorf("nextPow2(%d) = %d, want %d", c.in, got, c.want)
		}
	}
}

func testEstimatedSBBF(n uint, maxFp float64, t *testing.T) {
	// Empirical FP rate test: insert n distinct keys, probe with a
	// disjoint set, count false positives. SBBF's FP rate at the
	// Parquet sizing is typically within ~2x of the requested rate
	// on small filters (the cost of the cache-friendly clustering)
	// and converges as the filter grows, so we allow up to 3x maxFp
	// as the sanity ceiling.
	f := NewSBBFWithEstimates(n, maxFp)
	for i := uint(0); i < n; i++ {
		k := make([]byte, 8)
		binary.LittleEndian.PutUint64(k, uint64(i))
		f.Add(k)
	}
	const probes = 200_000
	fps := 0
	for i := uint(0); i < probes; i++ {
		k := make([]byte, 8)
		// Disjoint from inserted: top bit set so the inserted [0, n)
		// range never collides.
		binary.LittleEndian.PutUint64(k, uint64(i)|(1<<63))
		if f.Test(k) {
			fps++
		}
	}
	rate := float64(fps) / float64(probes)
	if rate > 3*maxFp {
		t.Errorf("SBBF false-positive rate too high: n=%d maxFp=%f got=%f", n, maxFp, rate)
	}
}

func TestSBBFEstimated1000_001(t *testing.T)    { testEstimatedSBBF(1000, 0.01, t) }
func TestSBBFEstimated10000_001(t *testing.T)   { testEstimatedSBBF(10000, 0.01, t) }
func TestSBBFEstimated100000_001(t *testing.T)  { testEstimatedSBBF(100000, 0.01, t) }
func TestSBBFEstimated10000_0001(t *testing.T)  { testEstimatedSBBF(10000, 0.001, t) }
func TestSBBFEstimated100000_0001(t *testing.T) { testEstimatedSBBF(100000, 0.001, t) }

// SBBF benchmarks compare FilterSBBF against BloomFilter on the same
// key set across two cache regimes:
//
//	small  - filter fits comfortably in L2 (~10 KB classical / ~32 KB SBBF)
//	medium - filter exceeds L3 (~36 MB classical / ~48 MB SBBF)
//
// Each regime is benched in two flavours:
//
//	Test([]byte)     - the full user-facing API; both filters hash internally
//	TestHash         - pre-hashed; isolates the probe from the hash cost
//
// The per-regime setup (especially the 30M-insert Medium setup) is
// cached via sync.Once so the cost is paid once per `go test -bench .`
// invocation rather than once per benchmark function.

type sbbfBenchSetup struct {
	bf       *BloomFilter
	sbbf     *FilterSBBF
	missKeys [][]byte
	hitKeys  [][]byte
}

func buildSBBFBenchSetup(nInsert uint, fp float64) *sbbfBenchSetup {
	bf := NewWithEstimates(nInsert, fp)
	sbbf := NewSBBFWithEstimates(nInsert, fp)
	rng := rand.New(rand.NewSource(0xC0FFEE))
	inserted := make([][]byte, nInsert)
	for i := uint(0); i < nInsert; i++ {
		k := make([]byte, 32)
		_, _ = rng.Read(k)
		inserted[i] = k
		bf.Add(k)
		sbbf.Add(k)
	}
	const nQuery = 50_000
	missKeys := make([][]byte, nQuery)
	for i := range missKeys {
		k := make([]byte, 32)
		_, _ = rng.Read(k)
		k[0] |= 0x80
		missKeys[i] = k
	}
	hitKeys := make([][]byte, nQuery)
	for i := range hitKeys {
		hitKeys[i] = inserted[rng.Intn(len(inserted))]
	}
	return &sbbfBenchSetup{bf: bf, sbbf: sbbf, missKeys: missKeys, hitKeys: hitKeys}
}

var (
	smallSetupOnce  sync.Once
	smallSetup      *sbbfBenchSetup
	mediumSetupOnce sync.Once
	mediumSetup     *sbbfBenchSetup
)

func smallBenchSetup() *sbbfBenchSetup {
	smallSetupOnce.Do(func() { smallSetup = buildSBBFBenchSetup(8_000, 0.01) })
	return smallSetup
}

func mediumBenchSetup() *sbbfBenchSetup {
	mediumSetupOnce.Do(func() { mediumSetup = buildSBBFBenchSetup(30_000_000, 0.01) })
	return mediumSetup
}

func benchBloomTest(b *testing.B, s *sbbfBenchSetup, hit bool) {
	keys := s.missKeys
	if hit {
		keys = s.hitKeys
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.bf.Test(keys[i%len(keys)])
	}
}

func benchSBBFTest(b *testing.B, s *sbbfBenchSetup, hit bool) {
	keys := s.missKeys
	if hit {
		keys = s.hitKeys
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.sbbf.Test(keys[i%len(keys)])
	}
}

// Pre-hashed equivalents strip MurmurHash3 from both sides so the
// comparison is purely the bit-test geometry.
func benchBloomTestHash(b *testing.B, s *sbbfBenchSetup, hit bool) {
	keys := s.missKeys
	if hit {
		keys = s.hitKeys
	}
	locs := make([][]uint64, len(keys))
	for i, k := range keys {
		locs[i] = Locations(k, s.bf.k)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.bf.TestLocations(locs[i%len(locs)])
	}
}

func benchSBBFTestHash(b *testing.B, s *sbbfBenchSetup, hit bool) {
	keys := s.missKeys
	if hit {
		keys = s.hitKeys
	}
	hashes := make([]uint64, len(keys))
	for i, k := range keys {
		hashes[i] = baseHashes(k)[0]
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		s.sbbf.TestHash(hashes[i%len(hashes)])
	}
}

func BenchmarkBloomTest_Small_Miss(b *testing.B) { benchBloomTest(b, smallBenchSetup(), false) }
func BenchmarkBloomTest_Small_Hit(b *testing.B)  { benchBloomTest(b, smallBenchSetup(), true) }
func BenchmarkSBBFTest_Small_Miss(b *testing.B)  { benchSBBFTest(b, smallBenchSetup(), false) }
func BenchmarkSBBFTest_Small_Hit(b *testing.B)   { benchSBBFTest(b, smallBenchSetup(), true) }

func BenchmarkBloomTest_Medium_Miss(b *testing.B) { benchBloomTest(b, mediumBenchSetup(), false) }
func BenchmarkBloomTest_Medium_Hit(b *testing.B)  { benchBloomTest(b, mediumBenchSetup(), true) }
func BenchmarkSBBFTest_Medium_Miss(b *testing.B)  { benchSBBFTest(b, mediumBenchSetup(), false) }
func BenchmarkSBBFTest_Medium_Hit(b *testing.B)   { benchSBBFTest(b, mediumBenchSetup(), true) }

func BenchmarkBloomTestHash_Medium_Miss(b *testing.B) {
	benchBloomTestHash(b, mediumBenchSetup(), false)
}
func BenchmarkBloomTestHash_Medium_Hit(b *testing.B) {
	benchBloomTestHash(b, mediumBenchSetup(), true)
}
func BenchmarkSBBFTestHash_Medium_Miss(b *testing.B) {
	benchSBBFTestHash(b, mediumBenchSetup(), false)
}
func BenchmarkSBBFTestHash_Medium_Hit(b *testing.B) {
	benchSBBFTestHash(b, mediumBenchSetup(), true)
}
