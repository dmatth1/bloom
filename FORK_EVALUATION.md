# Evaluation: FilterSBBF contribution to bits-and-blooms/bloom

This document evaluates the `FilterSBBF` change that lives on branch
`claude/quickbloom-bits-blooms-4HYBx` of this fork (a single commit,
`b0ea44c`, adding `sbbf.go` + `sbbf_test.go`) and assesses the likelihood
that upstream `bits-and-blooms/bloom` would accept it as a pull request.

## What's in the fork

One commit on `claude/quickbloom-bits-blooms-4HYBx`: adds `FilterSBBF`,
a Split Block Bloom Filter living alongside `BloomFilter`. Two new
files, no edits to existing code:

- `sbbf.go` (217 lines) — Parquet-spec SBBF: 256-bit blocks, K=8,
  power-of-2 block count, the spec salt vector. Reuses
  `baseHashes()[0]` so no new dependencies.
- `sbbf_test.go` (398 lines) — correctness (no-FN, FP-rate sanity at
  0.01/0.001, AddHash↔Add agreement, ClearAll/Equal/TestOrAdd), plus
  benchmarks vs `BloomFilter` on small + 30M-item regimes, with
  `sync.Once`-cached setup.

Tests pass. Quick bench on a Xeon @ 2.1 GHz, 32 KB L2-resident filter:

| Workload          | BloomFilter | FilterSBBF | Δ      |
|-------------------|------------:|-----------:|-------:|
| Small Miss        |    67.0 ns  |   49.4 ns  |  −26%  |
| Small Hit         |    58.5 ns  |   44.8 ns  |  −23%  |

The "several-x" wins claimed in the commit message only show up on
filters that exceed L3, where SBBF's one-cache-line probe dominates
`BloomFilter`'s K scattered probes. The small-regime numbers above are
the floor.

## What it's missing vs the existing `BloomFilter` surface

Upstream will compare type-for-type. Gaps:

- No `WriteTo` / `ReadFrom` / `MarshalBinary` / `UnmarshalBinary` /
  `GobEncode` / `GobDecode` / `UnmarshalJSON`
- No `Merge`, no `Copy`
- No `ApproximatedSize`
- Doesn't use `bitset.BitSet` (raw `[]uint32`), so no `BitSet()`
  accessor

The existing type ships a full serialization suite (PR #89 was
specifically about adding `BinaryMarshaler`). A parallel type without
any of that is the most obvious "not done" signal.

## Likelihood it lands: ~15–25% as-is

Factors against:

- **Repo is in maintenance mode.** Upstream has 9 commits in 2 years,
  5 in the last year. All maintenance: version bumps, README,
  big-endian hash fix, BinaryMarshaler. No major feature PR has landed
  in years.
- **Tight scope.** The package is "a BloomFilter," singular. Adding a
  second top-level type is a bigger architectural ask than improving
  the existing one, and the maintainer (Daniel Lemire) is opinionated
  and a known blocked-Bloom researcher — if SBBF were an obvious fit
  he'd likely have added it already.
- **Missing the serialization surface** (above). Without it, this type
  isn't usable in the contexts the existing one serves.
- **The Go scalar port is the weak version of the pitch.** The
  compelling number is the SIMD/AVX2 result in the companion
  `quickbloom` C library. A pure-Go SBBF wins ~25–40% on the cases
  reproducible locally, with bigger gains only on filters that exceed
  L3. That's a real win, but not "you must take this" territory for a
  stable library.
- **Adds Parquet-interop framing** (salt vector, power-of-2 blocks)
  which upstream has no other reason to care about.

Factors for:

- Algorithm is well-known and spec-defined — no novel design to debate.
- Test coverage is reasonably thorough and the patch is additive (zero
  risk to the existing type).
- The recent PRs that did land (BigEndian fix, BinaryMarshaler) suggest
  upstream will take well-scoped, well-tested contributions.

## Is the fork justified?

For local use (and as the Go-side companion to `quickbloom`'s C/SIMD
SBBF), yes — the algorithm choice is correct, the implementation is
clean, and the perf claim holds even in pure Go. For landing upstream
as-is, no. The realistic path to merge would be:

1. Add the full serialization suite +
   `Equal`/`Merge`/`Copy`/`ApproximatedSize` to match `BloomFilter`.
2. Land benchmarks showing the large-filter case where the cache-line
   argument actually pays off (the small-regime numbers undersell it).
3. Open it as an RFC issue first asking whether upstream would
   entertain a second filter type at all — given the repo's cadence, a
   600-line surprise PR is likely to sit.

Even with all that, ~40% feels generous. If the goal is to make the
work useful as a contribution, the higher-leverage move is probably to
publish it as a standalone `sbbf-go` module (or fold it into
`quickbloom` as a Go binding) rather than push it into
`bits-and-blooms/bloom`.
