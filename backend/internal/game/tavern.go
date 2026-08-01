package game

import "math/rand"

// -----------------------------------------------------------------------
// The Tavern is a non-combat location reached once, automatically,
// after a character clears Stage 2's finale (see handlers/game.go's
// handleDescend, which sets SaveState.AtTavern true on that specific
// transition instead of dropping the player straight into Stage 3).
// It's also the ONLY location a character can act from once their run
// is complete (state.IsRunComplete()) — see handleDescend and
// handleTavernLeave, which together enforce "AtTavern can never be
// cleared again" once RunComplete is true.
//
// Everything here is static reference/content data, mirroring the
// existing "fixed data, not earned/rolled" role Stages/Enemies/Weapons
// play elsewhere in this package. handlers/game.go's handleTavern*
// functions are what actually mutate a SaveState against this data.
// -----------------------------------------------------------------------

// GoldDropChancePercent and GoldDropAmount define the flat, class-
// agnostic gold economy: every defeated enemy has this percent chance
// of dropping this much gold. Rolled once per kill, immediately after
// the existing Mage-only Mana grant in grantVictoryRewards
// (handlers/game.go) — gold, unlike Mana, isn't class-gated, since
// every class can spend it in the tavern.
const (
	GoldDropChancePercent = 50
	GoldDropAmount        = 1
)

// RollGoldDrop rolls whether a just-defeated enemy dropped gold, using
// the same rollPercent-style 1-100 roll every other percent-chance
// mechanic in this package uses (see combat.go's ConditionOneHitKillChance
// handling) — kept here rather than reusing combat.go's unexported
// rollPercent so tavern.go doesn't need an internal cross-file
// dependency for one line of arithmetic.
func RollGoldDrop(rng *rand.Rand) int {
	if rng.Intn(100) < GoldDropChancePercent {
		return GoldDropAmount
	}
	return 0
}

// TavernPotions lists which of items.go's existing Potions are
// purchasable in the tavern, and at what gold cost. Deliberately reuses
// the same Potion IDs/definitions rather than minting tavern-only
// duplicates — a Vigor Tonic bought in the tavern is the exact same
// item as one found as a stage-finale reward, just acquired a
// different way.
var TavernPotions = map[string]int{
	"p_stat_tonic": 1,
	"p_hp_elixir":  2,
}

// ScrollSpells are permanent Mage spells NOT known from character
// creation (contrast game.MageSpells, the fixed starting kit) —
// learned one at a time by buying the matching scroll in the tavern.
// This is the "learn-a-spell flow" that spells.go's MageSpells doc
// comment explicitly called out as not existing; it now does, but
// only through this separate, opt-in list, so the base kit every Mage
// starts with stays exactly as fixed/unconditional as it always was.
//
// This is the full 7-spell POOL the tavern draws from — NOT what's on
// offer on any given visit. Every tavern visit only actually offers
// TavernScrollOfferCount (2) of these, chosen by RollTavernSpells and
// persisted on SaveState.CurrentTavernSpells so the same two stay on
// offer for the rest of that visit. See RollTavernSpells' doc comment
// for when a fresh pair gets rolled.
var ScrollSpells = []Spell{
	{
		ID:             "s_frost_lance",
		Name:           "Frost Lance",
		Description:    "A shard of ice hurled with bruising force — hits harder than Chain Lightning, at the same risk of missing.",
		ManaCost:       1,
		Kind:           SpellKindDamage,
		AutoHit:        false,
		DamageDieSides: 10,
		DamageDieCount: 2,
	},
	{
		ID:                 "s_greater_mend",
		Name:               "Greater Mend",
		Description:        "A deeper working of the same arcane mending — restores more than Arcane Mend, for the same mana.",
		ManaCost:           1,
		Kind:               SpellKindHeal,
		HealPercentOfMaxHP: 45,
	},
	{
		ID:             "s_earthen_spike",
		Name:           "Earthen Spike",
		Description:    "A jagged spike of stone erupts underfoot — a single heavy die, higher risk and higher reward than Frost Lance's two smaller ones.",
		ManaCost:       1,
		Kind:           SpellKindDamage,
		AutoHit:        false,
		DamageDieSides: 12,
		DamageDieCount: 1,
	},
	{
		ID:             "s_arc_burst",
		Name:           "Arc Burst",
		Description:    "Two quick pulses of raw arcane force — always connects like Firebolt, at a slightly higher ceiling.",
		ManaCost:       1,
		Kind:           SpellKindDamage,
		AutoHit:        true,
		DamageDieSides: 4,
		DamageDieCount: 2,
	},
	{
		ID:             "s_shadow_bolt",
		Name:           "Shadow Bolt",
		Description:    "A bolt of writhing shadow — comparable power to Chain Lightning, drawn from a darker, more unpredictable well.",
		ManaCost:       1,
		Kind:           SpellKindDamage,
		AutoHit:        false,
		DamageDieSides: 8,
		DamageDieCount: 2,
	},
	{
		ID:                 "s_radiant_ward",
		Name:               "Radiant Ward",
		Description:        "A shimmering ward of pale light knits your wounds — stronger than Arcane Mend, gentler than Greater Mend.",
		ManaCost:           1,
		Kind:               SpellKindHeal,
		HealPercentOfMaxHP: 35,
	},
	{
		ID:             "s_gale_edge",
		Name:           "Gale Edge",
		Description:    "A gust of wind given an edge — twice Firebolt's bite, and it still never misses.",
		ManaCost:       1,
		Kind:           SpellKindDamage,
		AutoHit:        true,
		DamageDieSides: 6,
		DamageDieCount: 2,
	},
}

// ScrollPrices maps each ScrollSpells entry to its gold cost, mirroring
// TavernPotions' shape. A separate map (not a field on Spell itself)
// since price is a tavern-specific concern, not an intrinsic property
// of the spell the way ManaCost is. Every scroll costs the same flat
// 2 gold regardless of which two the tavern happens to be offering —
// RollTavernSpells varies WHICH spells show up, never their price.
var ScrollPrices = map[string]int{
	"s_frost_lance":   2,
	"s_greater_mend":  2,
	"s_earthen_spike": 2,
	"s_arc_burst":     2,
	"s_shadow_bolt":   2,
	"s_radiant_ward":  2,
	"s_gale_edge":     2,
}

// TavernScrollOfferCount is how many of ScrollSpells' 7 entries are
// actually offered for sale on any single tavern visit — see
// RollTavernSpells.
const TavernScrollOfferCount = 2

// RollTavernSpells draws TavernScrollOfferCount distinct spell IDs at
// random from the full ScrollSpells pool. Called exactly once per
// tavern VISIT (handlers/game.go's handleDescend, at both places that
// set SaveState.AtTavern true — the Stage 2 finale waypoint and the
// Stage 5/DungeonComplete waypoint), never on every "tavern" menu
// re-display — the result is persisted onto
// SaveState.CurrentTavernSpells so re-opening the menu, or checking
// state mid-visit, keeps showing the same two spells until the player
// leaves and a later visit rolls again.
func RollTavernSpells(rng *rand.Rand) []string {
	indices := rng.Perm(len(ScrollSpells))
	count := TavernScrollOfferCount
	if count > len(indices) {
		count = len(indices)
	}
	ids := make([]string, 0, count)
	for _, idx := range indices[:count] {
		ids = append(ids, ScrollSpells[idx].ID)
	}
	return ids
}

// GetScrollSpell looks up a ScrollSpells entry by ID, mirroring
// GetSpell's (found, ok) pattern in spells.go.
func GetScrollSpell(id string) (Spell, bool) {
	for _, s := range ScrollSpells {
		if s.ID == id {
			return s, true
		}
	}
	return Spell{}, false
}

// GetKnownSpell looks up a spell a given character can actually cast:
// either their permanent starting kit (MageSpells) or a scroll they've
// already learned (state.LearnedSpells). Unlike GetSpell/GetScrollSpell
// above (which just check whether a spell ID is DEFINED anywhere),
// this is the check handleCast actually needs — "does THIS character
// know this spell" — so a Mage can never cast a scroll spell they
// haven't bought yet just because its ID is valid content.
func GetKnownSpell(state SaveState, id string) (Spell, bool) {
	if spell, ok := GetSpell(id); ok {
		return spell, true
	}
	for _, learnedID := range state.LearnedSpells {
		if learnedID != id {
			continue
		}
		if spell, ok := GetScrollSpell(id); ok {
			return spell, true
		}
	}
	return Spell{}, false
}

// MonsterLore holds one flavor/tactical-advice entry per EnemyArchetype
// (brute/stalker/caster — enemies.go), unlocked all-at-once by the
// tavern's "learn about spell effectiveness" service (handleTavernLore).
// Kept as static content rather than a real elemental-weakness combat
// system — enemies.go's archetypes don't carry resistances/weaknesses
// mechanically, so this is knowledge that helps a player reason about
// AutoHit vs. to-hit-roll spells against each archetype's actual stat
// profile, not a hidden damage multiplier.
var MonsterLore = []string{
	"Brutes (high CON, high HP, unremarkable DEX): a slow war of attrition favors you less than it favors them. Chain Lightning's bigger dice chew through their HP pool faster than Firebolt's smaller, safer bolt.",
	"Stalkers (high DEX, high AC): they're hard to pin down with a normal spell roll. Firebolt's auto-hit ignores their defenses entirely — reliable damage beats a bigger die you might not land.",
	"Casters (low HP, hits hard): the fight is short either way, so burst matters more than efficiency. Arcane Mend (or a learned Greater Mend) between casts can outlast their high damage rolls better than trying to out-race them.",
}

// ClearDeathMarksItemID and ClearDeathMarksPrice back a third tavern
// purchase, alongside TavernPotions/ScrollPrices above: wiping the
// account's death-mark count (database.RecordDeath's counter, NOT
// SaveState — see handlers/game.go's handleTavernBuy, which is the
// only place this ID is checked) for a flat gold cost. Kept as its
// own constant pair rather than folded into TavernPotions/ScrollPrices
// since it isn't an Inventory or LearnedSpells item — it doesn't grant
// anything a character HOLDS, it clears a DB-side counter directly.
const (
	ClearDeathMarksItemID = "cleanse_death_marks"
	ClearDeathMarksPrice  = 3
)

// TavernRiddle is one entry in the Riddles pool below. Answer is
// matched case-insensitively and trimmed (see handlers/game.go's
// handleTavernRiddle) so "Echo", "echo ", and "ECHO" all count as
// correct — a player shouldn't lose the reward over capitalization or
// a stray space.
type TavernRiddle struct {
	ID       string
	Question string
	Answer   string
}

// Riddles is a small pool; handleTavernRiddle picks one at random per
// tavern visit (not per attempt — see SaveState.TavernRiddleSolved,
// which caps the +3 mana reward to once per run regardless of how many
// times the riddle is re-rolled or the tavern re-visited).
var Riddles = []TavernRiddle{
	{
		ID:       "r_echo",
		Question: "I speak without a mouth and hear without ears. I have no body, but I come alive with wind. What am I?",
		Answer:   "echo",
	},
	{
		ID:       "r_candle",
		Question: "The more of me you take, the more you leave behind. What am I?",
		Answer:   "footsteps",
	},
	{
		ID:       "r_map",
		Question: "I have cities, but no houses; forests, but no trees; rivers, but no water. What am I?",
		Answer:   "map",
	},
	{
		ID:       "r_keys",
		Question: "I have keys but no locks, space but no room. You can enter, but not go outside. What am I?",
		Answer:   "keyboard",
	},
	{
		ID:       "r_shadow",
		Question: "It follows you all day, disappears at night, and grows longest when the light is weakest. What is it?",
		Answer:   "shadow",
	},
}

// RandomRiddle picks one Riddles entry. Deterministic given the same
// *rand.Rand, matching this package's existing convention (see
// combat.go's ResolveAttack) of never reaching for the global
// math/rand source directly.
func RandomRiddle(rng *rand.Rand) TavernRiddle {
	return Riddles[rng.Intn(len(Riddles))]
}

// The tavern's two gold-wagering games — blackjack and roulette — live
// in their own files (blackjack.go, roulette.go) rather than here,
// since each has enough self-contained rules (card/wheel mechanics,
// payout math) to warrant its own file, the same granularity this
// package already uses for combat.go/familiars.go/ambush.go rather
// than piling every mechanic into one file.

// GetRiddle looks up a Riddles entry by ID — needed because the riddle
// a player is currently being asked has to be re-identified on the
// ANSWER submission request (a stateless HTTP call can't just "remember"
// which one it asked), so SaveState needs to persist which riddle ID it
// last handed out. See SaveState.CurrentRiddleID.
func GetRiddle(id string) (TavernRiddle, bool) {
	for _, r := range Riddles {
		if r.ID == id {
			return r, true
		}
	}
	return TavernRiddle{}, false
}