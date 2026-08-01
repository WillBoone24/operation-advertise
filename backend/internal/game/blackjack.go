package game

import (
	"math/rand"
	"strconv"
)

// -----------------------------------------------------------------------
// Blackjack is one of the tavern's two gold-wagering games — roulette.go
// is the other. Together they replace tavern.go's earlier one-line
// coin-flip gamble with two real casino games. Both are wagered and paid
// out entirely in Gold, the one currency every class holds, with no
// spell/mana involvement anywhere in either game — a Mage gets no edge
// (or disadvantage) over a Fighter/Cleric/Ranger/Rogue sitting at the
// same table, matching Gold's existing class-agnostic role everywhere
// else in the tavern (see tavern.go's Gold doc comment).
//
// Cards are drawn from an INFINITE shoe, not a depleting 52-card deck:
// every draw (DrawBlackjackCard) is an independent, uniform pick across
// the 13 ranks below, with no memory of what's already been drawn. This
// keeps SaveState's persisted hand to just the ranks drawn so far and
// avoids having to serialize/track a shrinking deck across stateless
// HTTP requests (handleTavernBlackjack in handlers/game.go is a
// different call for every hit/stand) — a simplification, not a bug,
// and a standard approach for this style of single-table
// implementation. Suit is never tracked at all, since it has no effect
// on blackjack scoring.
// -----------------------------------------------------------------------

// BlackjackMaxRounds caps how many rounds of blackjack a single RUN can
// play — see SaveState.BlackjackRoundsPlayed, which is reset to 0 only
// where every other run-scoped tavern field is (state.go's Reset), so
// the cap holds across both tavern visits in a run (Stage 2 finale and
// Stage 5/DungeonComplete), not just one.
const BlackjackMaxRounds = 5

// BlackjackMinWager and BlackjackMaxWager bound a single round's wager.
const (
	BlackjackMinWager = 1
	BlackjackMaxWager = 10
)

// BlackjackNaturalPayoutNumerator/Denominator is the standard 3:2
// natural-blackjack payout (a 2-card 21). Kept as a fraction rather
// than a flat multiplier so integer gold amounts still divide sensibly
// (e.g. a 2-gold wager pays 3 gold, via 2*3/2) — see finishBlackjackRound
// in handlers/game.go for where this is actually applied.
const (
	BlackjackNaturalPayoutNumerator   = 3
	BlackjackNaturalPayoutDenominator = 2
)

// blackjackRanks is the 13 ranks of a single suit — suit is irrelevant
// to blackjack scoring (see this file's doc comment), so only rank is
// ever drawn or stored.
var blackjackRanks = []string{"A", "2", "3", "4", "5", "6", "7", "8", "9", "10", "J", "Q", "K"}

// DrawBlackjackCard draws one card (a rank string, e.g. "A", "10", "K")
// from the infinite shoe described in this file's doc comment.
func DrawBlackjackCard(rng *rand.Rand) string {
	return blackjackRanks[rng.Intn(len(blackjackRanks))]
}

// BlackjackHandValue scores a hand using the standard rule: Aces count
// as 11 unless that would bust the hand, in which case they drop to 1
// one at a time until the total is 21 or under (or every Ace has
// dropped). Face cards (J/Q/K) and "10" all count as 10.
func BlackjackHandValue(cards []string) int {
	total := 0
	aces := 0
	for _, c := range cards {
		switch c {
		case "A":
			total += 11
			aces++
		case "J", "Q", "K", "10":
			total += 10
		default:
			if v, err := strconv.Atoi(c); err == nil {
				total += v
			}
		}
	}
	for total > 21 && aces > 0 {
		total -= 10
		aces--
	}
	return total
}

// IsBlackjackNatural reports whether a freshly-dealt 2-card hand totals
// 21 (an Ace plus any 10-value card) — only meaningful before any hit,
// the same way a real "natural" only ever counts on the first two cards.
func IsBlackjackNatural(cards []string) bool {
	return len(cards) == 2 && BlackjackHandValue(cards) == 21
}

// DealerShouldHit applies the standard "hit until 17, stand on
// everything 17 or higher" dealer rule — including a soft 17 (no
// hard/soft distinction is made), the simplest common house rule and
// the one handleTavernBlackjack's stand branch loops on.
func DealerShouldHit(dealerCards []string) bool {
	return BlackjackHandValue(dealerCards) < 17
}