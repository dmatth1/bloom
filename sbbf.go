package bloom

// FilterSBBF is a Split Block Bloom Filter (SBBF), a cache-efficient
// blocked Bloom filter introduced by Putze, Sanders, and Singler in
// "Cache-, Hash- and Space-Efficient Bloom Filters" (2007) and adopted
// as the standard Bloom filter format by Apache Parquet.
//
// Where the classical BloomFilter probes _k_ independent bit positions
// scattered across the bitset (typically _k_ cache lines per Test on a
// true positive), SBBF clusters all bits for a single key into one
// 256-bit block on one cache line. A Test reads exactly one cache line
// regardless of whether the key is present, which on large filters
// (filter size >> L3) is dominated by the memory latency saved.
//
// Trade-offs versus BloomFilter:
//   - SBBF uses a fixed _k_ of 8 bits per key; you choose the number of
//     blocks rather than (_m_, _k_). NewSBBFWithEstimates picks the
//     block count from (_n_, _fp_) using the Parquet sizing.
//   - SBBF requires the number of blocks to be a power of two so the
//     block index can be derived by a bit mask rather than a modulo.
//   - At the same target false-positive rate, SBBF uses slightly more
//     bits per key than the optimal classical filter (the bit-budget
//     cost of the cache-friendly layout); the trade is one cache miss
//     per probe instead of up to _k_.
//
// FilterSBBF is not a drop-in serialization replacement for
// BloomFilter: the on-disk layouts differ and the two cannot be merged.
type FilterSBBF struct {
	blocks    []uint32 // length = numBlocks * 8
	numBlocks uint64
	blockMask uint64 // numBlocks - 1; numBlocks is required to be a power of two
}

// sbbfSalt is the bit-position salt vector from the Apache Parquet
// SBBF specification (see github.com/apache/parquet-format/blob/master
// /BloomFilter.md). These eight constants — one per uint32 lane within
// a 256-bit block — are fixed: changing them produces a filter whose
// bitset is not interoperable with other Parquet SBBF implementations.
var sbbfSalt = [8]uint32{
	0x47b6137b, 0x44974d91, 0x8824ad5b, 0xa2b7289d,
	0x705495c7, 0x2df1424b, 0x9efc4947, 0x5c6bfb31,
}

// NewSBBF returns a new SBBF with numBlocks 256-bit blocks. numBlocks
// must be a power of two; the function panics otherwise. Each block
// occupies 32 bytes, so the filter uses 32 * numBlocks bytes of memory.
// Callers that prefer to size by item count and target false-positive
// rate should use NewSBBFWithEstimates.
func NewSBBF(numBlocks uint64) *FilterSBBF {
	if numBlocks == 0 || numBlocks&(numBlocks-1) != 0 {
		panic("bloom: NewSBBF requires numBlocks to be a non-zero power of two")
	}
	return &FilterSBBF{
		blocks:    make([]uint32, numBlocks*8),
		numBlocks: numBlocks,
		blockMask: numBlocks - 1,
	}
}

// NewSBBFWithEstimates returns an SBBF sized to hold approximately _n_
// items at the requested false-positive rate _fp_. The block count is
// derived from the same bit-budget formula EstimateParameters uses for
// BloomFilter, then rounded up to the next power of two.
func NewSBBFWithEstimates(n uint, fp float64) *FilterSBBF {
	m, _ := EstimateParameters(n, fp)
	numBlocks := nextPow2((uint64(m) + 255) / 256)
	return NewSBBF(numBlocks)
}

// nextPow2 returns the smallest power of two greater than or equal to x.
// nextPow2(0) returns 1.
func nextPow2(x uint64) uint64 {
	if x <= 1 {
		return 1
	}
	x--
	x |= x >> 1
	x |= x >> 2
	x |= x >> 4
	x |= x >> 8
	x |= x >> 16
	x |= x >> 32
	return x + 1
}

// NumBlocks returns the number of 256-bit blocks in the filter.
func (f *FilterSBBF) NumBlocks() uint64 {
	return f.numBlocks
}

// Cap returns the total bit capacity, _m_, of the filter
// (NumBlocks * 256).
func (f *FilterSBBF) Cap() uint64 {
	return f.numBlocks * 256
}

// K returns the number of hash functions used by the FilterSBBF, which
// is fixed at 8 by the Parquet SBBF specification (one bit per uint32
// lane within a 256-bit block).
func (f *FilterSBBF) K() uint {
	return 8
}

// Add data to the FilterSBBF. Returns the filter (allows chaining).
func (f *FilterSBBF) Add(data []byte) *FilterSBBF {
	h := baseHashes(data)
	return f.AddHash(h[0])
}

// AddString to the FilterSBBF. Returns the filter (allows chaining).
func (f *FilterSBBF) AddString(data string) *FilterSBBF {
	return f.Add([]byte(data))
}

// AddHash inserts a pre-computed 64-bit hash. Callers that already
// have a uint64 hash for the key can skip the murmur step.
func (f *FilterSBBF) AddHash(hash uint64) *FilterSBBF {
	blockIdx := (hash >> 32) & f.blockMask
	key := uint32(hash)
	base := blockIdx * 8
	for i := 0; i < 8; i++ {
		bit := (key * sbbfSalt[i]) >> 27
		f.blocks[base+uint64(i)] |= uint32(1) << bit
	}
	return f
}

// Test returns true if the data is in the FilterSBBF, false otherwise.
// If true, the result might be a false positive. If false, the data
// is definitely not in the set.
func (f *FilterSBBF) Test(data []byte) bool {
	h := baseHashes(data)
	return f.TestHash(h[0])
}

// TestString returns true if the string is in the FilterSBBF, false
// otherwise. If true, the result might be a false positive. If false,
// the string is definitely not in the set.
func (f *FilterSBBF) TestString(data string) bool {
	return f.Test([]byte(data))
}

// TestHash probes the filter using a pre-computed 64-bit hash.
func (f *FilterSBBF) TestHash(hash uint64) bool {
	blockIdx := (hash >> 32) & f.blockMask
	key := uint32(hash)
	base := blockIdx * 8
	for i := 0; i < 8; i++ {
		bit := (key * sbbfSalt[i]) >> 27
		if (f.blocks[base+uint64(i)]>>bit)&1 == 0 {
			return false
		}
	}
	return true
}

// TestAndAdd is equivalent to calling Test(data) then Add(data). The
// filter is written to unconditionally: even if the element is present,
// the corresponding bits are still set. See also TestOrAdd. Returns
// the result of Test.
func (f *FilterSBBF) TestAndAdd(data []byte) bool {
	h := baseHashes(data)
	present := f.TestHash(h[0])
	f.AddHash(h[0])
	return present
}

// TestAndAddString is equivalent to calling Test(string) then
// Add(string). The filter is written to unconditionally: even if the
// string is present, the corresponding bits are still set. See also
// TestOrAdd. Returns the result of Test.
func (f *FilterSBBF) TestAndAddString(data string) bool {
	return f.TestAndAdd([]byte(data))
}

// TestOrAdd is equivalent to calling Test(data) then, if not present,
// Add(data). If the element is already in the filter, then the filter
// is unchanged. Returns the result of Test.
func (f *FilterSBBF) TestOrAdd(data []byte) bool {
	h := baseHashes(data)
	present := f.TestHash(h[0])
	if !present {
		f.AddHash(h[0])
	}
	return present
}

// TestOrAddString is equivalent to calling Test(string) then, if not
// present, Add(string). If the string is already in the filter, then
// the filter is unchanged. Returns the result of Test.
func (f *FilterSBBF) TestOrAddString(data string) bool {
	return f.TestOrAdd([]byte(data))
}

// ClearAll clears all the data in a FilterSBBF, removing all keys.
// Returns the filter (allows chaining).
func (f *FilterSBBF) ClearAll() *FilterSBBF {
	for i := range f.blocks {
		f.blocks[i] = 0
	}
	return f
}

// Equal tests for the equality of two FilterSBBFs. Two filters are
// equal if they have the same number of blocks and identical block
// contents.
func (f *FilterSBBF) Equal(g *FilterSBBF) bool {
	if f.numBlocks != g.numBlocks {
		return false
	}
	for i, b := range f.blocks {
		if b != g.blocks[i] {
			return false
		}
	}
	return true
}
