package game

import "math/rand"

// -----------------------------------------------------------------------
// Roulette is the tavern's other gold-wagering game — see blackjack.go's
// doc comment for the shared "gold only, no spells, no class edge"
// reasoning both games follow. European-style: a single 0, no American
// double-zero, pockets 1-36 split red/black exactly as a real wheel —
// no additional house edge beyond the single green zero, which loses
// every red/black/odd/even bet and wins only if it's the exact
// straight-up pick.
//
// Unlike blackjack, a single spin resolves entirely within one request
// — there's no multi-step hand to persist across calls, so
// handleTavernRoulette (handlers/game.go) needs no SaveState fields the
// way BlackjackActive/BlackjackWager/etc. exist for blackjack.
// -----------------------------------------------------------------------

// RouletteMaxRounds caps how many spins a single RUN can play — see
// BlackjackMaxRounds's doc comment for the same "holds across both
// tavern visits" reasoning, kept as its own constant (rather than
// shared with blackjack) since the two games are tracked with separate
// SaveState counters and could, in principle, ever get different caps.
const RouletteMaxRounds = 5

// RouletteMinWager and RouletteMaxWager bound a single spin's wager,
// mirroring BlackjackMinWager/BlackjackMaxWager.
const (
	RouletteMinWager = 1
	RouletteMaxWager = 10
)

// RouletteStraightPayoutMultiplier is the standard single-number payout
// (35:1 — a winning straight-up bet returns the wager plus 35x the
// wager on top). RouletteEvenMoneyPayoutMultiplier covers red/black and
// odd/even (1:1, i.e. a win doubles the wager).
const (
	RouletteStraightPayoutMultiplier  = 35
	RouletteEvenMoneyPayoutMultiplier = 1
)

// rouletteRedNumbers are the 18 red pockets on a standard European
// wheel; every other non-zero pocket (1-36) is black. 0 is green and
// wins nothing for a red/black or odd/even bet — the wheel's one house
// edge, same role tavern.go's ClearDeathMarksItemID/etc. play as a flat
// gold sink elsewhere in the tavern.
var rouletteRedNumbers = map[int]bool{
	1: true, 3: true, 5: true, 7: true, 9: true, 12: true, 14: true, 16: true,
	18: true, 19: true, 21: true, 23: true, 25: true, 27: true, 30: true,
	32: true, 34: true, 36: true,
}

// SpinRoulette spins the wheel once and returns the winning pocket,
// 0-36 inclusive.
func SpinRoulette(rng *rand.Rand) int {
	return rng.Intn(37)
}

// RouletteIsRed reports whether a pocket is red (false for 0 and every
// black pocket).
func RouletteIsRed(pocket int) bool {
	return rouletteRedNumbers[pocket]
}

// RouletteIsBlack reports whether a pocket is black (false for 0 and
// every red pocket) — a dedicated function rather than just
// "!RouletteIsRed(pocket)", so 0's "neither" case reads explicitly at
// every call site instead of relying on the reader to remember that
// RouletteIsRed(0) being false doesn't make 0 black.
func RouletteIsBlack(pocket int) bool {
	return pocket != 0 && !rouletteRedNumbers[pocket]
}

// RouletteIsOdd and RouletteIsEven mirror the red/black pair above — 0
// is neither odd nor even for betting purposes, the same house-edge
// rule red/black applies to it.
func RouletteIsOdd(pocket int) bool {
	return pocket != 0 && pocket%2 == 1
}

func RouletteIsEven(pocket int) bool {
	return pocket != 0 && pocket%2 == 0
}