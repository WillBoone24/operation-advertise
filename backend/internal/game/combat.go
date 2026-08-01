package game

import (
	"fmt"
	"math/rand"
)

// AttackProfile is the combat-facing shape both a player's equipped
// weapon and an enemy's built-in attack collapse into before
// ResolveAttack runs. Introduced so combat.go has exactly ONE
// resolution function that doesn't care whether it's resolving a
// player's swing or an enemy's — it never touches Item or Enemy
// directly, only this shape. BuildPlayerAttackProfile and
// BuildEnemyAttackProfile below are the two (and only two) places that
// construct one.
type AttackProfile struct {
	AttackStatModifier int // added to the d20 attack roll
	DamageStatModifier int // added to the damage roll
	DamageDieSides      int
	DamageDieCount      int
	DamageBonus         int

	Condition      SpecialCondition
	ConditionValue int
}

// BuildPlayerAttackProfile derives an AttackProfile from a character's
// class + equipped weapon. Player damage dice are intentionally
// standardized (1d8) rather than defined per-weapon — items.go's
// Weapons only vary stat mods and conditions, not raw damage dice, so
// this is the one place "how big is a player's hit" is decided, kept
// separate from item data so tuning it later doesn't mean editing all
// 15 weapon entries.
func BuildPlayerAttackProfile(class Class, weapon Item, stats Stats) (AttackProfile, error) {
	statMod, err := resolveStatModifier(class.AttackStat, stats)
	if err != nil {
		return AttackProfile{}, err
	}

	return AttackProfile{
		AttackStatModifier: statMod,
		DamageStatModifier: statMod,
		DamageDieSides:     8,
		DamageDieCount:     1,
		DamageBonus:        0,
		Condition:          weapon.Condition,
		ConditionValue:     weapon.ConditionValue,
	}, nil
}

// BuildEnemyAttackProfile derives an AttackProfile from an Enemy's own
// definition (enemies.go already carries damage dice + condition
// directly, since enemies don't have separate equippable items).
func BuildEnemyAttackProfile(enemy Enemy) (AttackProfile, error) {
	statMod, err := resolveStatModifier(enemy.AttackStat, enemy.Stats)
	if err != nil {
		return AttackProfile{}, err
	}

	return AttackProfile{
		AttackStatModifier: statMod,
		DamageStatModifier: statMod,
		DamageDieSides:     enemy.DamageDieSides,
		DamageDieCount:     enemy.DamageDieCount,
		DamageBonus:        enemy.DamageBonus,
		Condition:          enemy.Condition,
		ConditionValue:     enemy.ConditionValue,
	}, nil
}

// resolveStatModifier looks up "str" or "dex" on a Stats block and
// returns its D&D-style modifier. The only place a bare stat-name
// string gets turned into an actual number, so a typo'd stat name
// (e.g. a future content entry accidentally writing "strength"
// instead of "str") fails loudly here instead of silently resolving
// to a zero modifier everywhere it's used.
func resolveStatModifier(statName string, stats Stats) (int, error) {
	switch statName {
	case "str":
		return StatModifier(stats.STR), nil
	case "dex":
		return StatModifier(stats.DEX), nil
	default:
		return 0, fmt.Errorf("game: unknown attack stat %q", statName)
	}
}

// AttackResult is the fully-resolved outcome of one attack — pure
// data, no narrative text. The future handler layer (Day 6-7) turns
// this into player-facing strings; combat.go's job stops at producing
// correct numbers, matching the project's existing separation of
// "logic produces data, something else renders it" (see
// TerminalEmulator.js's SECURITY NOTE on a similar boundary on the
// frontend).
type AttackResult struct {
	AttackRoll int // raw d20 result, 1-20, before modifiers — useful for display ("you rolled a 17")
	Hit        bool
	Crit       bool

	DamageDealt int

	// Killed is true if this attack reduced the defender to 0 HP OR
	// triggered a one-hit-kill condition. RolledOneHitKill
	// distinguishes which of those actually happened, since a
	// one-hit-kill on an enemy already at 1 HP is a coincidence worth
	// telling apart from "the dagger's edge found something vital."
	Killed           bool
	RolledOneHitKill bool

	// LifestealHealAmount is how much HP the attacker regained, 0 if
	// their weapon/attack has no lifesteal condition or the attack
	// missed (lifesteal only triggers on a landed hit, same rule as
	// one-hit-kill — see ResolveAttack).
	LifestealHealAmount int

	// AppliedPoison, AppliedBurn, and AppliedStun report whether this
	// attack's weapon condition (items.go's ConditionPoison/Burn/Stun)
	// just triggered — same "rolled after a confirmed hit" rule as
	// RolledOneHitKill. combat.go itself never mutates InCombat's
	// ailment counters directly (it stays pure, same convention as
	// everywhere else in this file); the caller (handlers/game.go) is
	// what actually sets InCombat's *Turns fields when one of these is
	// true.
	AppliedPoison bool
	AppliedBurn   bool
	AppliedStun   bool
}

// critThreshold returns the minimum d20 roll that counts as a
// critical hit for a given AttackProfile: 20 by default, lowered by
// ConditionBonusCritRange's value (e.g. ConditionValue: 2 means 18+
// crits). Floored at 15 — a weapon/enemy that could crit on anything
// lower would make the AttackRoll number nearly meaningless, which is
// a balance problem worth a hard guardrail here rather than trusting
// every future content entry to self-regulate.
func critThreshold(profile AttackProfile) int {
	threshold := 20
	if profile.Condition == ConditionBonusCritRange {
		threshold -= profile.ConditionValue
	}
	if threshold < 15 {
		threshold = 15
	}
	return threshold
}

// ResolveAttack rolls and resolves a single attack from an attacker's
// AttackProfile against a defender's armor class and current HP.
// Deterministic given the same *rand.Rand and inputs — callers pass in
// their own rand.Rand (rather than this function reaching for the
// global math/rand source) so a future test can seed it and assert on
// exact outcomes, the same reasoning internal/util/random.go already
// applies to ID generation.
//
// Resolution order, matching standard d20-style rules:
//  1. Roll to hit: d20 + AttackStatModifier vs. defenderAC.
//  2. On a miss, stop here — no damage, no conditions trigger.
//  3. On a hit, roll damage; a natural roll >= critThreshold doubles
//     the dice portion of damage (not the flat modifier, standard
//     D&D-style crit rule).
//  4. One-hit-kill and lifesteal conditions are checked ONLY after a
//     confirmed hit — a miss never triggers either, regardless of
//     ConditionValue.
func ResolveAttack(rng *rand.Rand, profile AttackProfile, defenderAC int, defenderCurrentHP int) AttackResult {
	roll := rollD20(rng)
	totalAttack := roll + profile.AttackStatModifier

	if totalAttack < defenderAC {
		return AttackResult{AttackRoll: roll, Hit: false}
	}

	return resolveHit(rng, profile, roll, defenderCurrentHP)
}

// ResolveGuaranteedAttack resolves an attack that skips the
// d20-vs-AC check entirely and always lands — used by Ranger's
// Steady Aim (SaveState.InCombat.NextAttackGuaranteedHit), mirroring
// how Spell.AutoHit already bypasses the same check for Firebolt. A
// d20 is still rolled (AttackRoll stays meaningful for display and
// crit-threshold purposes), it just never gets compared to AC.
func ResolveGuaranteedAttack(rng *rand.Rand, profile AttackProfile, defenderCurrentHP int) AttackResult {
	roll := rollD20(rng)
	return resolveHit(rng, profile, roll, defenderCurrentHP)
}

// resolveHit is the shared "a hit has been confirmed" resolution path
// used by both ResolveAttack (after winning the roll-vs-AC check) and
// ResolveGuaranteedAttack (which skips that check entirely) — crit,
// damage, one-hit-kill, lifesteal, and ailment rolls are identical
// either way once a hit is confirmed, so this is the ONE place that
// logic lives rather than being duplicated across both entry points.
func resolveHit(rng *rand.Rand, profile AttackProfile, roll int, defenderCurrentHP int) AttackResult {
	crit := roll >= critThreshold(profile)

	damage := rollDamage(rng, profile.DamageDieSides, profile.DamageDieCount)
	if crit {
		// Standard D&D-style crit: double the DICE portion only, flat
		// modifiers (stat mod + weapon damage bonus) are added once.
		damage += rollDamage(rng, profile.DamageDieSides, profile.DamageDieCount)
	}
	damage += profile.DamageStatModifier + profile.DamageBonus
	if damage < 1 {
		// A negative-modifier attacker (e.g. a low-STR Mage swinging
		// something they shouldn't) still deals at minimum 1 damage on
		// a confirmed hit — a hit that deals 0 or negative damage
		// reads as a bug to a player, not a balance nuance.
		damage = 1
	}

	result := AttackResult{
		AttackRoll:  roll,
		Hit:         true,
		Crit:        crit,
		DamageDealt: damage,
	}

	if profile.Condition == ConditionOneHitKillChance {
		if rollPercent(rng) <= profile.ConditionValue {
			result.RolledOneHitKill = true
			result.Killed = true
		}
	}

	if !result.Killed && damage >= defenderCurrentHP {
		result.Killed = true
	}

	if profile.Condition == ConditionLifesteal {
		result.LifestealHealAmount = (damage * profile.ConditionValue) / 100
	}

	switch profile.Condition {
	case ConditionPoison:
		result.AppliedPoison = rollPercent(rng) <= profile.ConditionValue
	case ConditionBurn:
		result.AppliedBurn = rollPercent(rng) <= profile.ConditionValue
	case ConditionStun:
		result.AppliedStun = rollPercent(rng) <= profile.ConditionValue
	}

	return result
}

// ArmorClass computes a defender's AC: the standard D&D baseline of
// 10 + DEX modifier. Armor's contribution is already folded into
// SaveState.EffectiveStats() (via StatMods on DEX for pieces like
// a_robe/a_cloak) by the time Stats reaches this function — AC has no
// separate "armor bonus" term here on top of that, avoiding double-
// counting the same piece of armor's protection twice.
func ArmorClass(stats Stats) int {
	return 10 + StatModifier(stats.DEX)
}

// RogueSneakAttackBonusChance is the flat percentage chance Sneak
// Attack (classes.go) adds to a Rogue's first LANDED attack in a
// fight, if that attack hits an enemy still at full HP. Stacks
// additively with — doesn't replace — whatever
// ConditionOneHitKillChance the equipped weapon already rolls
// separately inside resolveHit above, since this is a once-per-fight
// CLASS trait, not a per-swing weapon property.
const RogueSneakAttackBonusChance = 15

// RollSneakAttack rolls Sneak Attack's bonus one-hit-kill chance.
// Exported so handlers/game.go can call it without reaching into
// this package's private rollPercent.
func RollSneakAttack(rng *rand.Rand) bool {
	return rollPercent(rng) <= RogueSneakAttackBonusChance
}

// rollD20, rollDamage, and rollPercent are the only three functions in
// this package that call into math/rand directly — every other piece
// of dice-rolling logic composes these rather than rolling dice itself
// inline, so the "what does a die roll actually look like" question
// has exactly one place it's answered.

func rollD20(rng *rand.Rand) int {
	return rng.Intn(20) + 1
}

func rollDamage(rng *rand.Rand, sides, count int) int {
	total := 0
	for i := 0; i < count; i++ {
		total += rng.Intn(sides) + 1
	}
	return total
}

// rollPercent returns 1-100 inclusive, for percentage-chance
// conditions (one-hit-kill%, and anything similar added later).
func rollPercent(rng *rand.Rand) int {
	return rng.Intn(100) + 1
}