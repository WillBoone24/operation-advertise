package game

// ItemSlot identifies which equip slot an item occupies. A character
// can have one weapon and one armor equipped at a time — no ring/boot/
// helmet slots, per the "isn't hyper complex" scope decision.
type ItemSlot string

const (
	SlotWeapon ItemSlot = "weapon"
	SlotArmor  ItemSlot = "armor"
)

// SpecialCondition names a non-stat-mod effect a weapon can carry.
// Kept as a small closed set of string constants (not a generic
// scripting/effect system) so combat.go can switch on a known list
// rather than needing to interpret arbitrary effect data.
type SpecialCondition string

const (
	// ConditionNone means the item only affects stats, no special
	// combat behavior.
	ConditionNone SpecialCondition = ""

	// ConditionOneHitKillChance grants a flat percentage chance (see
	// Item.ConditionValue) that a successful hit kills the target
	// outright, regardless of remaining HP. Rolled by combat.go AFTER
	// a normal hit already lands — a miss never triggers it.
	ConditionOneHitKillChance SpecialCondition = "one_hit_kill_chance"

	// ConditionBonusCritRange lowers the roll threshold needed to land
	// a critical hit (normally a natural 20). E.g. ConditionValue: 2
	// means 19-20 both crit, not just 20.
	ConditionBonusCritRange SpecialCondition = "bonus_crit_range"

	// ConditionLifesteal heals the wielder for a percentage of damage
	// dealt on a successful hit. ConditionValue is that percentage
	// (0-100).
	ConditionLifesteal SpecialCondition = "lifesteal"

	// ConditionPoison, ConditionBurn, and ConditionStun are the three
	// status ailments a player's attack can inflict on an enemy.
	// ConditionValue is a percentage chance (0-100) of the ailment
	// applying, rolled once per confirmed hit — same "checked only
	// after a hit already lands" rule ConditionOneHitKillChance
	// follows (see combat.go's ResolveAttack). Duration for all three
	// is StatusEffectDuration rounds, set on InCombat when applied and
	// ticked down once per round in handlers/game.go's combat loop —
	// see InCombat's doc comment in state.go for exactly where each
	// one is read.
	//
	//   - ConditionPoison weakens the enemy's own attack roll while
	//     active (a flat penalty applied in BuildEnemyAttackProfile's
	//     caller, not baked into the enemy's stats themselves).
	//   - ConditionBurn deals small, fixed passive damage at the start
	//     of each round it's active, independent of any attack roll.
	//   - ConditionStun skips the enemy's counterattack entirely for
	//     each round it's active.
	ConditionPoison SpecialCondition = "poison"
	ConditionBurn   SpecialCondition = "burn"
	ConditionStun   SpecialCondition = "stun"
)

// StatusEffectDuration is how many of the enemy's own turns a
// poison/burn/stun application lasts once it lands. One shared
// constant (not per-condition) keeps all three easy to reason about
// together — tune here if any needs to feel longer/shorter later.
const StatusEffectDuration = 3

// PoisonAttackPenalty is the flat penalty applied to an enemy's
// AttackStatModifier while poisoned — see BuildEnemyAttackProfile's
// caller in handlers/game.go, which is where this actually gets
// subtracted (combat.go's own profile builders stay unaware of
// status effects, matching AttackProfile's existing "pure data"
// role).
const PoisonAttackPenalty = 3

// BurnDamagePerTurn is the fixed damage burn deals each round it's
// active, independent of any hit/miss roll.
const BurnDamagePerTurn = 2

// Item is the shared shape for both weapons and armor. Weapons and
// armor live in two separate maps below (rather than one combined
// list) because they're looked up differently everywhere else in the
// codebase: a character has exactly one of each, addressed by slot,
// and keeping them in separate maps means a lookup can never
// accidentally hand back the wrong slot's item.
type Item struct {
	ID   string   `json:"id"`
	Name string   `json:"name"`
	Slot ItemSlot `json:"slot"`

	// StatMods are added directly to the wielder's base Stats at
	// combat-resolution time (see combat.go). Zero-value fields mean
	// "no bonus to that stat" — there is no separate "has a STR bonus"
	// flag, a zero mod is simply a no-op when added.
	StatMods Stats `json:"stat_mods"`

	// HPMod is a flat max-HP bonus, separate from Stats since HP isn't
	// one of the three tracked ability scores.
	HPMod int `json:"hp_mod"`

	// RestrictedToClass limits a weapon to one class, matching the
	// project's "5 classes, 3 weapon options per class" scope — a
	// Fighter cannot equip a Mage's wand. Empty string means
	// unrestricted (true for all armor pieces, which are shared across
	// classes rather than class-specific like weapons).
	RestrictedToClass ClassID `json:"restricted_to_class,omitempty"`

	Condition      SpecialCondition `json:"condition,omitempty"`
	ConditionValue int              `json:"condition_value,omitempty"`
}

// Weapons holds all 15 class-restricted weapons (3 per class × 5
// classes). Keyed by ID for O(1) lookup from a character's
// SaveState.Equipped.Weapon field (state.go, Day 4) without a linear
// scan.
//
// Design note on why weapons are class-restricted at all: it's what
// makes class choice matter mechanically, not just cosmetically — a
// Rogue's identity is "high one-hit-kill odds," which only holds if a
// Fighter can't just equip the Rogue's dagger and get the same odds
// with better base STR. Armor, by contrast, is NOT class-restricted
// (see Armor below) — that's a deliberate asymmetry, not an oversight:
// armor represents "how tanky are you willing to be," which is a valid
// choice for any class, whereas weapons represent class-specific combat
// style.
var Weapons = map[string]Item{
	// --- Fighter: three power/STR-leaning options ---
	"w_fighter_longsword": {
		ID: "w_fighter_longsword", Name: "Longsword", Slot: SlotWeapon,
		StatMods: Stats{STR: 2}, RestrictedToClass: ClassFighter,
	},
	"w_fighter_greatsword": {
		ID: "w_fighter_greatsword", Name: "Greatsword", Slot: SlotWeapon,
		StatMods: Stats{STR: 4}, RestrictedToClass: ClassFighter,
		Condition: ConditionBonusCritRange, ConditionValue: 1,
	},
	"w_fighter_warhammer": {
		ID: "w_fighter_warhammer", Name: "Warhammer", Slot: SlotWeapon,
		StatMods: Stats{STR: 3, CON: 1}, RestrictedToClass: ClassFighter,
		Condition: ConditionStun, ConditionValue: 25,
	},

	// --- Rogue: DEX-leaning, one-hit-kill-flavored ---
	"w_rogue_dagger": {
		ID: "w_rogue_dagger", Name: "Dagger", Slot: SlotWeapon,
		StatMods: Stats{DEX: 2}, RestrictedToClass: ClassRogue,
		Condition: ConditionPoison, ConditionValue: 40,
	},
	"w_rogue_shortsword": {
		ID: "w_rogue_shortsword", Name: "Shortsword", Slot: SlotWeapon,
		StatMods: Stats{DEX: 3}, RestrictedToClass: ClassRogue,
		Condition: ConditionOneHitKillChance, ConditionValue: 5,
	},
	"w_rogue_twinblades": {
		ID: "w_rogue_twinblades", Name: "Twin Blades", Slot: SlotWeapon,
		StatMods: Stats{DEX: 4}, RestrictedToClass: ClassRogue,
		Condition: ConditionOneHitKillChance, ConditionValue: 10,
	},

	// --- Mage: low STR isn't relevant to a wand, mods lean DEX (used
	// as the mage's ranged-attack stat, see combat.go) ---
	"w_mage_wand": {
		ID: "w_mage_wand", Name: "Wand", Slot: SlotWeapon,
		StatMods: Stats{DEX: 2}, RestrictedToClass: ClassMage,
		Condition: ConditionBurn, ConditionValue: 40,
	},
	"w_mage_staff": {
		ID: "w_mage_staff", Name: "Staff", Slot: SlotWeapon,
		StatMods: Stats{DEX: 3, CON: 1}, RestrictedToClass: ClassMage,
	},
	"w_mage_grimoire": {
		ID: "w_mage_grimoire", Name: "Grimoire", Slot: SlotWeapon,
		StatMods: Stats{DEX: 4}, RestrictedToClass: ClassMage,
		Condition: ConditionBonusCritRange, ConditionValue: 2,
	},

	// --- Cleric: CON-leaning with a lifesteal option, matching the
	// class's support identity ---
	"w_cleric_mace": {
		ID: "w_cleric_mace", Name: "Mace", Slot: SlotWeapon,
		StatMods: Stats{STR: 2}, RestrictedToClass: ClassCleric,
	},
	"w_cleric_flail": {
		ID: "w_cleric_flail", Name: "Flail", Slot: SlotWeapon,
		StatMods: Stats{STR: 2, CON: 1}, RestrictedToClass: ClassCleric,
		Condition: ConditionLifesteal, ConditionValue: 15,
	},
	"w_cleric_warstaff": {
		ID: "w_cleric_warstaff", Name: "War Staff", Slot: SlotWeapon,
		StatMods: Stats{STR: 3, CON: 1}, RestrictedToClass: ClassCleric,
	},

	// --- Ranger: DEX-leaning, consistent rather than flashy ---
	"w_ranger_bow": {
		ID: "w_ranger_bow", Name: "Shortbow", Slot: SlotWeapon,
		StatMods: Stats{DEX: 2}, RestrictedToClass: ClassRanger,
	},
	"w_ranger_longbow": {
		ID: "w_ranger_longbow", Name: "Longbow", Slot: SlotWeapon,
		StatMods: Stats{DEX: 3}, RestrictedToClass: ClassRanger,
	},
	"w_ranger_crossbow": {
		ID: "w_ranger_crossbow", Name: "Crossbow", Slot: SlotWeapon,
		StatMods: Stats{DEX: 3, STR: 1}, RestrictedToClass: ClassRanger,
		Condition: ConditionBonusCritRange, ConditionValue: 1,
	},
}

// Armor holds the 5 equipment pieces. Unrestricted by class (see the
// design note on Weapons above) — any character can equip any of
// these.
var Armor = map[string]Item{
	"a_leather": {
		ID: "a_leather", Name: "Leather Armor", Slot: SlotArmor,
		StatMods: Stats{DEX: 1}, HPMod: 4,
	},
	"a_chainmail": {
		ID: "a_chainmail", Name: "Chainmail", Slot: SlotArmor,
		StatMods: Stats{CON: 1}, HPMod: 8,
	},
	"a_plate": {
		ID: "a_plate", Name: "Plate Armor", Slot: SlotArmor,
		StatMods: Stats{CON: 2}, HPMod: 12,
	},
	"a_robe": {
		ID: "a_robe", Name: "Enchanted Robe", Slot: SlotArmor,
		StatMods: Stats{DEX: 2}, HPMod: 2,
		Condition: ConditionBonusCritRange, ConditionValue: 1,
	},
	"a_cloak": {
		ID: "a_cloak", Name: "Shadow Cloak", Slot: SlotArmor,
		StatMods: Stats{DEX: 1, CON: 1}, HPMod: 6,
	},

	// --- Journey (Part 2) armor — one per region finale, stages 6-10
	// (see stages.go's JourneyStages). Ascending power same as the
	// dungeon's five pieces above, picking up roughly where a_plate
	// leaves off since these are earned strictly after it.
	"a_woodland_garb": {
		ID: "a_woodland_garb", Name: "Woodland Garb", Slot: SlotArmor,
		StatMods: Stats{DEX: 2}, HPMod: 6,
	},
	"a_windswept_mail": {
		ID: "a_windswept_mail", Name: "Windswept Mail", Slot: SlotArmor,
		StatMods: Stats{STR: 1, CON: 2}, HPMod: 10,
	},
	"a_ancient_ward": {
		ID: "a_ancient_ward", Name: "Ancient Ward", Slot: SlotArmor,
		StatMods: Stats{DEX: 2, CON: 2}, HPMod: 10,
		Condition: ConditionBonusCritRange, ConditionValue: 1,
	},
	"a_valley_plate": {
		ID: "a_valley_plate", Name: "Valley Plate", Slot: SlotArmor,
		StatMods: Stats{CON: 3}, HPMod: 16,
	},
	"a_mire_shroud": {
		ID: "a_mire_shroud", Name: "Mire Shroud", Slot: SlotArmor,
		StatMods: Stats{STR: 1, DEX: 2, CON: 2}, HPMod: 20,
		Condition: ConditionLifesteal, ConditionValue: 10,
	},
}

// GetWeapon and GetArmor mirror GetClass's (found, ok) pattern — item
// IDs referenced from a character's SaveState (or a future
// /api/game/action request body) originate from either previously-
// trusted server data (fine) or client input (e.g. an "equip" command
// argument, NOT fine to trust blindly), so every lookup site needs a
// clean way to detect "no such item" and reject rather than panic.

func GetWeapon(id string) (Item, bool) {
	item, ok := Weapons[id]
	return item, ok
}

func GetArmor(id string) (Item, bool) {
	item, ok := Armor[id]
	return item, ok
}

// PotionKind distinguishes what a potion does when consumed. Kept as
// its own small type (mirroring SpecialCondition) rather than reusing
// Item's Condition field — a potion's effect happens once, immediately,
// and outside combat resolution entirely, so it doesn't belong in the
// same enum as weapon/armor combat conditions.
type PotionKind string

const (
	// PotionKindStat permanently boosts one randomly-chosen stat
	// (STR/DEX/CON) by a random amount — see handleUse in
	// handlers/game.go for the actual roll (3-5 inclusive).
	// Deliberately not fixed here as a static value: which stat and
	// how much are per-use rolls, not per-item data.
	PotionKindStat PotionKind = "stat"

	// PotionKindHP restores CurrentHP to the drinker's effective max
	// HP. A one-time full heal, not a HPMod-style permanent bonus —
	// permanent max HP still only comes from equipped armor.
	PotionKindHP PotionKind = "hp"
)

// Potion is a one-time-use consumable, held in Inventory alongside
// armor IDs (see SaveState.Inventory's doc comment) but never
// equipped — GetPotion is consulted by handleUse, never by
// handleEquip, so a potion ID can never accidentally land in the
// Equipped slot the way an unknown-item bug might otherwise allow.
type Potion struct {
	ID   string     `json:"id"`
	Name string     `json:"name"`
	Kind PotionKind `json:"kind"`
}

// Potions holds the 2 consumable types. Only obtainable as a stage
// finale reward (see stages.go's RewardPotionID), same distribution
// model as Armor's RewardArmorID — no shop, no random drops.
var Potions = map[string]Potion{
	"p_stat_tonic": {ID: "p_stat_tonic", Name: "Vigor Tonic", Kind: PotionKindStat},
	"p_hp_elixir":  {ID: "p_hp_elixir", Name: "Healing Elixir", Kind: PotionKindHP},
}

func GetPotion(id string) (Potion, bool) {
	potion, ok := Potions[id]
	return potion, ok
}