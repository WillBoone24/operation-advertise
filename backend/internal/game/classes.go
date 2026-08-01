package game

// Stats is the shared stat block used by both player characters and
// enemies, so combat.go (Day 5) can resolve attacks between the two
// without needing two parallel stat shapes. Deliberately D&D-flavored
// (STR/DEX/CON) rather than a generic "attack/defense" pair, per the
// project's stated "dnd feel" goal.
//
// Values are the RAW ability scores (roughly 8-18 range, D&D-style),
// not pre-computed modifiers. combat.go derives modifiers from these
// at resolution time via Modifier() below — keeping the raw score as
// the stored value (rather than storing a precomputed modifier) means
// a future equipment bonus that adds directly to a stat (e.g. "+2
// STR") stays correct without needing a second derived field to keep
// in sync.
type Stats struct {
	STR int `json:"str"`
	DEX int `json:"dex"`
	CON int `json:"con"`
}

// StatModifier converts a raw ability score into a D&D-style modifier:
// floor((score-10)/2). combat.go calls this on whichever of STR/DEX/CON
// is relevant to a given roll (e.g. STR for a melee attack, DEX for an
// AC-based defense check).
func StatModifier(score int) int {
	if score >= 10 {
		return (score - 10) / 2
	}
	// Integer division truncates toward zero in Go, which is wrong for
	// negative modifiers (e.g. a score of 7 should give -2, not -1).
	// This branch handles that explicitly rather than relying on Go's
	// truncation behavior to accidentally produce the right answer.
	return -((11 - score) / 2)
}

// ClassID identifies one of the five playable classes. Stored as a
// string (not an int enum) in SaveState/save_data so a save file is
// human-readable and stays valid across any future reordering of the
// classes slice below.
type ClassID string

const (
	ClassFighter ClassID = "fighter"
	ClassRogue   ClassID = "rogue"
	ClassMage    ClassID = "mage"
	ClassCleric  ClassID = "cleric"
	ClassRanger  ClassID = "ranger"
)

// Class describes one playable class: its starting stats, starting HP,
// and a single signature ability. Kept to ONE ability per class
// (rather than a spell list or ability tree) per the project's explicit
// "isn't hyper complex" scope decision — the class identity comes from
// stat distribution + weapon access (see items.go) + this one ability,
// not from a large skill system.
type Class struct {
	ID          ClassID `json:"id"`
	Name        string  `json:"name"`
	Description string  `json:"description"`
	BaseStats   Stats   `json:"base_stats"`
	BaseMaxHP   int     `json:"base_max_hp"`

	// AttackStat names which of Stats (STR or DEX) drives this class's
	// attack rolls and damage — explicit rather than inferred from the
	// class's stat distribution, mirroring Enemy.AttackStat in
	// enemies.go exactly, so combat.go's resolveStatModifier can treat
	// players and enemies identically instead of needing two different
	// lookup rules.
	AttackStat string `json:"attack_stat"` // "str" or "dex"

	// AbilityName and AbilityDescription are display-only for now —
	// e.g. shown by a future "look"/"whoami"-style command. The actual
	// mechanical effect of each ability is implemented in combat.go
	// (Day 5), keyed off ClassID, not stored as data here. Keeping
	// flavor text and mechanical logic in separate files matches the
	// project's existing separation (models describe shape, handlers/
	// logic implement behavior).
	AbilityName        string `json:"ability_name"`
	AbilityDescription string `json:"ability_description"`

	// StartingWeaponID references an entry in the Weapons map
	// (items.go). Every class starts equipped with one of its own
	// three class-restricted weapons — see items.go's doc comment on
	// why weapons are class-restricted at all.
	StartingWeaponID string `json:"starting_weapon_id"`
}

// Classes is the fixed, authoritative list of the five playable
// classes. Package-level and immutable by convention (nothing in this
// codebase should mutate entries in this slice at runtime — a
// character's CURRENT stats live in SaveState, computed FROM this base
// data plus equipment, never by editing this data itself).
//
// Stat totals are deliberately balanced to sum to the same value
// (34) across all five classes, distributed differently per class
// identity. This is a starting point, not a final balance pass —
// Day 12/13 (playtesting) is where these numbers actually get proven
// out or adjusted.
var Classes = []Class{
	{
		ID:                 ClassFighter,
		Name:               "Fighter",
		Description:        "A frontline warrior built to take and deal heavy melee damage.",
		BaseStats:          Stats{STR: 16, DEX: 10, CON: 15},
		BaseMaxHP:          40,
		AbilityName:        "Second Wind",
		AbilityDescription: "Once per stage, heal a portion of max HP instead of attacking.",
		StartingWeaponID:   "w_fighter_longsword",
		AttackStat:         "str",
	},
	{
		ID:                 ClassRogue,
		Name:               "Rogue",
		Description:        "Fast and precise, trading raw power for a real chance at instant kills.",
		BaseStats:          Stats{STR: 10, DEX: 17, CON: 12},
		BaseMaxHP:          30,
		AbilityName:        "Sneak Attack",
		AbilityDescription: "The first attack against a full-HP enemy gets bonus one-hit-kill odds.",
		StartingWeaponID:   "w_rogue_dagger",
		AttackStat:         "dex",
	},
	{
		ID:                 ClassMage,
		Name:               "Mage",
		Description:        "Fragile but hits hardest at range, at the cost of low HP.",
		BaseStats:          Stats{STR: 8, DEX: 12, CON: 10},
		BaseMaxHP:          25,
		AbilityName:        "Firebolt",
		AbilityDescription: "A ranged attack that ignores the enemy's armor bonus to defense.",
		StartingWeaponID:   "w_mage_wand",
		AttackStat:         "dex",
	},
	{
		ID:                 ClassCleric,
		Name:               "Cleric",
		Description:        "A durable support class that can heal mid-fight.",
		BaseStats:          Stats{STR: 13, DEX: 10, CON: 14},
		BaseMaxHP:          35,
		AbilityName:        "Mend",
		AbilityDescription: "Once per stage, heal HP instead of attacking (smaller than Second Wind, but not tied to a threshold).",
		StartingWeaponID:   "w_cleric_mace",
		AttackStat:         "str",
	},
	{
		ID:                 ClassRanger,
		Name:               "Ranger",
		Description:        "Balanced and consistent, the safest class to learn combat on.",
		BaseStats:          Stats{STR: 12, DEX: 15, CON: 12},
		BaseMaxHP:          33,
		AbilityName:        "Steady Aim",
		AbilityDescription: "Trade a turn to guarantee the next attack roll hits.",
		StartingWeaponID:   "w_ranger_bow",
		AttackStat:         "dex",
	},
}

// GetClass looks up a Class by ID. Returns (Class{}, false) rather than
// panicking on an unknown ID — callers (the future /api/game/create
// handler) need to turn "unknown class" into a normal 400 response to
// the player, not a server crash, since ClassID here originates from
// untrusted client input.
func GetClass(id ClassID) (Class, bool) {
	for _, c := range Classes {
		if c.ID == id {
			return c, true
		}
	}
	return Class{}, false
}