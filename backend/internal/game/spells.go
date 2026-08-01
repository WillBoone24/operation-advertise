package game

import "math/rand"

// SpellKind distinguishes what a spell does when cast, mirroring
// PotionKind's role for potions (items.go) — a small closed set kept
// separate from SpecialCondition, since a spell resolves entirely
// within handleCast (handlers/game.go) rather than as a weapon-style
// combat modifier.
type SpellKind string

const (
	// SpellKindDamage strikes the current enemy for the spell's dice.
	// See Spell.AutoHit for the two different ways that damage can be
	// resolved.
	SpellKindDamage SpellKind = "damage"

	// SpellKindHeal restores a percentage of the caster's effective
	// max HP. Never overheals past max — same clamping rule as a
	// PotionKindHP potion.
	SpellKindHeal SpellKind = "heal"
)

// Spell is a permanent, always-known Mage ability. Unlike weapons and
// armor (items.go) or potions, spells are never earned, equipped, or
// consumed as inventory — every Mage character knows the full
// MageSpells list from character creation onward. The only thing
// gating how often one can be cast is Mana (see SaveState.Mana): a
// flat ManaCost per cast, earned back at a flat 1-per-victory rate
// (handlers/game.go's handleAttack/handleCast).
type Spell struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	ManaCost    int       `json:"mana_cost"`
	Kind        SpellKind `json:"kind"`

	// --- SpellKindDamage fields ---

	// AutoHit true means this spell bypasses the normal d20-vs-AC
	// attack roll entirely and always lands. This is Firebolt's
	// long-standing class-ability flavor text ("ignores the enemy's
	// armor bonus to defense" — see classes.go's Mage entry) finally
	// made mechanical, rather than two independent descriptions of
	// the same idea that could drift out of sync.
	AutoHit        bool `json:"auto_hit,omitempty"`
	DamageDieSides int  `json:"damage_die_sides,omitempty"`
	DamageDieCount int  `json:"damage_die_count,omitempty"`

	// --- SpellKindHeal fields ---

	// HealPercentOfMaxHP restores this percentage (0-100) of the
	// caster's effective max HP, rounded down. A percentage rather
	// than a flat number so the heal scales with armor-derived max HP
	// gained over a run instead of becoming trivial by Stage 5.
	HealPercentOfMaxHP int `json:"heal_percent_of_max_hp,omitempty"`
}

// MageSpells is the fixed, permanent spell list every Mage character
// starts with — no per-character selection, this slice IS the Mage's
// starting kit, the same "fixed data, not earned/rolled" role Classes
// plays for class definitions and Stages plays for the dungeon layout.
// A character CAN grow beyond this list now, via the tavern's scroll
// purchases (see game/tavern.go's ScrollSpells and SaveState.LearnedSpells)
// — GetSpell below only ever resolves this fixed starting kit, never a
// learned scroll; game.GetKnownSpell (tavern.go) is what checks both.
var MageSpells = []Spell{
	{
		ID:             "s_firebolt",
		Name:           "Firebolt",
		Description:    "A bolt of raw fire that always finds its mark, ignoring the enemy's defenses.",
		ManaCost:       1,
		Kind:           SpellKindDamage,
		AutoHit:        true,
		DamageDieSides: 6,
		DamageDieCount: 1,
	},
	{
		ID:             "s_chain_lightning",
		Name:           "Chain Lightning",
		Description:    "A crackling arc of lightning — harder to land than a firebolt, but hits much harder.",
		ManaCost:       1,
		Kind:           SpellKindDamage,
		AutoHit:        false,
		DamageDieSides: 8,
		DamageDieCount: 2,
	},
	{
		ID:                 "s_arcane_mend",
		Name:               "Arcane Mend",
		Description:        "Channels a fraction of your own arcane reserves into closing your wounds.",
		ManaCost:           1,
		Kind:               SpellKindHeal,
		HealPercentOfMaxHP: 25,
	},
}

// GetSpell looks up a Spell by ID, mirroring GetClass/GetWeapon/
// GetArmor/GetPotion's (found, ok) pattern — a spell_id in a
// POST /api/game/action body originates from client input and needs a
// clean "no such spell" rejection path, not a panic.
func GetSpell(id string) (Spell, bool) {
	for _, s := range MageSpells {
		if s.ID == id {
			return s, true
		}
	}
	return Spell{}, false
}

// ResolveAutoHitSpellDamage rolls damage for an AutoHit spell (e.g.
// Firebolt). There's no attack-roll-vs-AC step the way ResolveAttack
// has one — it always lands — but it still routes through the same
// rollDamage dice helper as every other damage source in this
// package, so "how big is a die roll" (combat.go's doc comment on
// rollD20/rollDamage/rollPercent) stays answered in exactly one
// place. statMod is the caster's class.AttackStat modifier, added the
// same way a weapon attack's damage stat modifier is.
func ResolveAutoHitSpellDamage(rng *rand.Rand, spell Spell, statMod int) int {
	damage := rollDamage(rng, spell.DamageDieSides, spell.DamageDieCount) + statMod
	if damage < 1 {
		// Same floor ResolveAttack applies — a spell that lands for 0
		// or negative damage reads as a bug, not a balance nuance.
		damage = 1
	}
	return damage
}
