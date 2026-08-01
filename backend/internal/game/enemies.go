package game

// EnemyArchetype identifies one of the three enemy types. Kept
// separate from DangerTier (below) so the two can vary independently —
// a "Brute" is still recognizably a Brute whether it's the weak or
// elite version, same as a player's ClassID doesn't change based on
// their gear.
type EnemyArchetype string

const (
	ArchetypeBrute   EnemyArchetype = "brute"   // STR-based melee, high HP
	ArchetypeStalker EnemyArchetype = "stalker" // DEX-based melee, crit/kill-chance flavored
	ArchetypeCaster  EnemyArchetype = "caster"  // DEX-based ranged, lower HP, hits harder

	// --- Journey (Part 2, stages 6-10 — see stages.go's JourneyStages
	// doc comment) archetypes. These are content-only groupings, same
	// as Brute/Stalker/Caster above — they don't change how combat
	// resolves, only which flavor/lore text an enemy falls under.
	ArchetypeForest EnemyArchetype = "forest" // Greenwood Trail
	ArchetypePlains EnemyArchetype = "plains" // Rolling Plains
	ArchetypeWoods  EnemyArchetype = "woods"  // Ancient Woods
	ArchetypeValley EnemyArchetype = "valley" // Shadow Valley
	ArchetypeMire   EnemyArchetype = "mire"   // Black Mire
)

// DangerTier identifies one of the three difficulty variants within an
// archetype. Ordered low to high so callers (stages.go) can reason
// about "is this tier at least Elite" with a simple comparison instead
// of string matching, if that's ever needed.
type DangerTier int

const (
	TierLesser DangerTier = iota
	TierStandard
	TierElite
)

// Enemy is the enemy-side equivalent of Class (classes.go) + a
// class-restricted weapon (items.go) rolled into one definition,
// rather than two separate pieces — enemies aren't player-controlled,
// so there's no need to model "an enemy's equipment" as a swappable,
// separately-persisted thing. Everything about how an enemy fights is
// just baked into its Enemy entry.
type Enemy struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Archetype   EnemyArchetype `json:"archetype"`
	Tier        DangerTier     `json:"tier"`

	Stats Stats `json:"stats"`
	MaxHP int   `json:"max_hp"`

	// AttackStat names which of Stats (STR or DEX) drives this
	// enemy's attack rolls and damage — explicit rather than inferred
	// from archetype, so combat.go never has to hardcode "Brutes use
	// STR" as an assumption buried in the resolution logic. See the
	// matching AttackStat field added to Class's combat-facing sibling
	// in combat.go's PlayerAttackProfile.
	AttackStat string `json:"attack_stat"` // "str" or "dex"

	// DamageDieSides/DamageDieCount/DamageBonus define this enemy's
	// damage roll: DamageDieCount dice of DamageDieSides sides, plus a
	// flat DamageBonus. E.g. 1d6+2 is {DamageDieSides:6,
	// DamageDieCount:1, DamageBonus:2}. Mirrors how a player weapon's
	// damage will be expressed once combat.go defines that shape.
	DamageDieSides int `json:"damage_die_sides"`
	DamageDieCount int `json:"damage_die_count"`
	DamageBonus    int `json:"damage_bonus"`

	// Condition/ConditionValue reuse items.go's SpecialCondition enum
	// so Elite-tier enemies can carry the same kind of danger a
	// player's best weapons do (a Stalker Elite with an one-hit-kill
	// chance, mirroring the Rogue's identity) — same type, same
	// resolution path in combat.go, no separate "enemy special
	// effects" system needed.
	Condition      SpecialCondition `json:"condition,omitempty"`
	ConditionValue int              `json:"condition_value,omitempty"`

	// Passive, if true, means this combatant never counterattacks —
	// used only by the final boss's first phase (boss.go's Menacing
	// Egg), which would otherwise make the fight's opening round
	// unfairly punishing. No entry in the regular Enemies list below
	// sets this; handlers/game.go's resolveEnemyCounterattack is what
	// actually checks it.
	Passive bool `json:"passive,omitempty"`
}

// Enemies is the fixed list of all 9 variants (3 archetypes × 3
// tiers). Stat/HP scaling across tiers is deliberately steep-ish
// (roughly +40-50% HP and +2 stat points per tier step) so "Elite"
// actually feels dangerous rather than a token label — Day 12/13
// playtesting is where this gets proven or walked back.
var Enemies = []Enemy{
	// --- Brute: STR-based melee, high HP, low finesse ---
	{
		ID: "brute_lesser", Name: "Cave Rat Brute", Archetype: ArchetypeBrute, Tier: TierLesser,
		Description:    "A hunched, snarling thing built for absorbing hits, not avoiding them.",
		Stats:          Stats{STR: 12, DEX: 8, CON: 12},
		MaxHP:          14,
		AttackStat:     "str",
		DamageDieSides: 6, DamageDieCount: 1, DamageBonus: 1,
	},
	{
		ID: "brute_standard", Name: "Iron-Fisted Brute", Archetype: ArchetypeBrute, Tier: TierStandard,
		Description:    "Scarred knuckles and a temper to match — it hits like it's trying to end the fight in one swing.",
		Stats:          Stats{STR: 15, DEX: 9, CON: 14},
		MaxHP:          22,
		AttackStat:     "str",
		DamageDieSides: 8, DamageDieCount: 1, DamageBonus: 2,
	},
	{
		ID: "brute_elite", Name: "Warlord Brute", Archetype: ArchetypeBrute, Tier: TierElite,
		Description:    "It has survived every fight it's ever been in. That is not a coincidence.",
		Stats:          Stats{STR: 18, DEX: 10, CON: 16},
		MaxHP:          34,
		AttackStat:     "str",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 3,
		Condition: ConditionBonusCritRange, ConditionValue: 2,
	},

	// --- Stalker: DEX-based melee, crit/kill-chance flavored ---
	{
		ID: "stalker_lesser", Name: "Shadow Stalker", Archetype: ArchetypeStalker, Tier: TierLesser,
		Description:    "Quick, skittish, and always aiming for an opening.",
		Stats:          Stats{STR: 9, DEX: 13, CON: 9},
		MaxHP:          10,
		AttackStat:     "dex",
		DamageDieSides: 4, DamageDieCount: 1, DamageBonus: 1,
	},
	{
		ID: "stalker_standard", Name: "Nightblade Stalker", Archetype: ArchetypeStalker, Tier: TierStandard,
		Description:    "It doesn't want a fair fight. It wants a fast one.",
		Stats:          Stats{STR: 10, DEX: 16, CON: 11},
		MaxHP:          16,
		AttackStat:     "dex",
		DamageDieSides: 6, DamageDieCount: 1, DamageBonus: 2,
		Condition: ConditionOneHitKillChance, ConditionValue: 5,
	},
	{
		ID: "stalker_elite", Name: "Death's Hand Stalker", Archetype: ArchetypeStalker, Tier: TierElite,
		Description:    "By the time you see it move, it's already decided whether you live.",
		Stats:          Stats{STR: 11, DEX: 19, CON: 12},
		MaxHP:          24,
		AttackStat:     "dex",
		DamageDieSides: 8, DamageDieCount: 1, DamageBonus: 2,
		Condition: ConditionOneHitKillChance, ConditionValue: 12,
	},

	// --- Caster: DEX-based ranged, low HP, hits hard ---
	{
		ID: "caster_lesser", Name: "Flickering Acolyte", Archetype: ArchetypeCaster, Tier: TierLesser,
		Description:    "Barely holding a spell together, but a spell is still a spell.",
		Stats:          Stats{STR: 7, DEX: 12, CON: 8},
		MaxHP:          9,
		AttackStat:     "dex",
		DamageDieSides: 6, DamageDieCount: 1, DamageBonus: 2,
	},
	{
		ID: "caster_standard", Name: "Ember Conjurer", Archetype: ArchetypeCaster, Tier: TierStandard,
		Description:    "Fire that doesn't ask permission before it spreads.",
		Stats:          Stats{STR: 8, DEX: 14, CON: 10},
		MaxHP:          14,
		AttackStat:     "dex",
		DamageDieSides: 8, DamageDieCount: 1, DamageBonus: 3,
	},
	{
		ID: "caster_elite", Name: "Voidbound Conjurer", Archetype: ArchetypeCaster, Tier: TierElite,
		Description:    "Whatever it's channeling isn't from anywhere you want to ask about.",
		Stats:          Stats{STR: 9, DEX: 17, CON: 12},
		MaxHP:          20,
		AttackStat:     "dex",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 4,
		Condition: ConditionLifesteal, ConditionValue: 20,
	},

	// -------------------------------------------------------------
	// Journey (Part 2) enemies — stages 6-10, see stages.go's
	// JourneyStages doc comment. Five regions × three tiers each,
	// same "one archetype per region, Lesser/Standard/Elite within
	// it" shape as the dungeon's Brute/Stalker/Caster above, just
	// keyed to a region instead of a combat role. Numbers climb well
	// past brute_elite/stalker_elite/caster_elite's ceiling —
	// deliberately: this content is reached only AFTER a character
	// has already beaten the full 5-stage dungeon (including the
	// final boss), so "as tough as the hardest thing already
	// beaten" would read as no progression at all. Region-over-region
	// scaling roughly follows a 1.0 / 1.3 / 1.7 / 2.8 / 4.0 HP curve
	// and a 1.0 / 1.2 / 1.5 / 2.3 / 3.2 damage curve (Shadow Valley
	// is the intentional difficulty wall, Black Mire the endgame
	// ceiling) — baked directly into each entry's stat block rather
	// than a separate runtime multiplier, matching how the dungeon's
	// own Lesser->Elite scaling is just hand-tuned per entry, not
	// computed. First-draft numbers, same as BossPhases — tune during
	// playtesting.
	// -------------------------------------------------------------

	// --- Region 1: Greenwood Trail — hopeful, peaceful ---
	{
		ID: "forest_scout", Name: "Forest Scout", Archetype: ArchetypeForest, Tier: TierLesser,
		Description:    "Quick and cautious, more interested in your pack than a fair fight.",
		Stats:          Stats{STR: 9, DEX: 14, CON: 10},
		MaxHP:          16,
		AttackStat:     "dex",
		DamageDieSides: 6, DamageDieCount: 1, DamageBonus: 2,
	},
	{
		ID: "forest_hunter", Name: "Forest Hunter", Archetype: ArchetypeForest, Tier: TierStandard,
		Description:    "It knows this trail better than the maps ever will.",
		Stats:          Stats{STR: 11, DEX: 16, CON: 12},
		MaxHP:          22,
		AttackStat:     "dex",
		DamageDieSides: 8, DamageDieCount: 1, DamageBonus: 3,
	},
	{
		ID: "forest_warden", Name: "Forest Warden", Archetype: ArchetypeForest, Tier: TierElite,
		Description:    "Something old put this thing here to watch the road. It's still watching.",
		Stats:          Stats{STR: 13, DEX: 15, CON: 16},
		MaxHP:          30,
		AttackStat:     "dex",
		DamageDieSides: 8, DamageDieCount: 1, DamageBonus: 4,
		Condition: ConditionBonusCritRange, ConditionValue: 1,
	},

	// --- Region 2: Rolling Plains — adventure, organized enemies ---
	{
		ID: "plains_raider", Name: "Highway Raider", Archetype: ArchetypePlains, Tier: TierLesser,
		Description:    "Opportunistic and armed — the wide-open road makes for easy pickings.",
		Stats:          Stats{STR: 14, DEX: 10, CON: 13},
		MaxHP:          20,
		AttackStat:     "str",
		DamageDieSides: 8, DamageDieCount: 1, DamageBonus: 2,
	},
	{
		ID: "plains_marauder", Name: "Marauder", Archetype: ArchetypePlains, Tier: TierStandard,
		Description:    "Rides in fast, hits hard, and doesn't stick around to see if you get up.",
		Stats:          Stats{STR: 16, DEX: 12, CON: 14},
		MaxHP:          27,
		AttackStat:     "str",
		DamageDieSides: 8, DamageDieCount: 1, DamageBonus: 3,
		Condition: ConditionBonusCritRange, ConditionValue: 1,
	},
	{
		ID: "plains_captain", Name: "Bandit Captain", Archetype: ArchetypePlains, Tier: TierElite,
		Description:    "Commands the raiders working this stretch of road, and fights like it.",
		Stats:          Stats{STR: 18, DEX: 13, CON: 16},
		MaxHP:          36,
		AttackStat:     "str",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 4,
		Condition: ConditionOneHitKillChance, ConditionValue: 6,
	},

	// --- Region 3: Ancient Woods — mysterious, emotional midpoint,
	// second familiar earned here (see stages.go's
	// AncientWoodsFamiliarStage/Part and handlers/game.go's
	// handleDescend) ---
	{
		ID: "woods_sprite", Name: "Woodland Sprite", Archetype: ArchetypeWoods, Tier: TierLesser,
		Description:    "Small, quick, and made almost entirely of misdirection.",
		Stats:          Stats{STR: 8, DEX: 16, CON: 10},
		MaxHP:          20,
		AttackStat:     "dex",
		DamageDieSides: 8, DamageDieCount: 1, DamageBonus: 3,
	},
	{
		ID: "woods_dryad", Name: "Ancient Dryad", Archetype: ArchetypeWoods, Tier: TierStandard,
		Description:    "The forest itself seems to lean in around it, feeding it strength.",
		Stats:          Stats{STR: 9, DEX: 18, CON: 13},
		MaxHP:          28,
		AttackStat:     "dex",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 4,
		Condition: ConditionLifesteal, ConditionValue: 15,
	},
	{
		ID: "woods_guardian", Name: "Forest Guardian", Archetype: ArchetypeWoods, Tier: TierElite,
		Description:    "Bark for skin and centuries of patience — it doesn't need to hurry to win.",
		Stats:          Stats{STR: 17, DEX: 12, CON: 20},
		MaxHP:          46,
		AttackStat:     "str",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 5,
		Condition: ConditionBonusCritRange, ConditionValue: 2,
	},

	// --- Region 4: Shadow Valley — oppressive, major difficulty
	// spike (see stages.go's difficulty-tier comment) ---
	{
		ID: "valley_stalker", Name: "Shadow Stalker", Archetype: ArchetypeValley, Tier: TierLesser,
		Description:    "By the time you register movement, it's already gone somewhere worse.",
		Stats:          Stats{STR: 12, DEX: 20, CON: 14},
		MaxHP:          38,
		AttackStat:     "dex",
		DamageDieSides: 8, DamageDieCount: 1, DamageBonus: 6,
		Condition: ConditionOneHitKillChance, ConditionValue: 10,
	},
	{
		ID: "valley_beast", Name: "Corrupted Beast", Archetype: ArchetypeValley, Tier: TierStandard,
		Description:    "Whatever it used to be, the valley finished the job a long time ago.",
		Stats:          Stats{STR: 20, DEX: 13, CON: 18},
		MaxHP:          52,
		AttackStat:     "str",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 7,
		Condition: ConditionBonusCritRange, ConditionValue: 2,
	},
	{
		ID: "valley_knight", Name: "Fallen Knight", Archetype: ArchetypeValley, Tier: TierElite,
		Description:    "Its armor still bears a crest from a kingdom no one remembers anymore.",
		Stats:          Stats{STR: 19, DEX: 15, CON: 20},
		MaxHP:          64,
		AttackStat:     "str",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 8,
		Condition: ConditionLifesteal, ConditionValue: 20,
	},

	// --- Region 5: Black Mire — despair, endgame survival, the
	// strongest non-boss enemies in the game ---
	{
		ID: "mire_horror", Name: "Mire Horror", Archetype: ArchetypeMire, Tier: TierLesser,
		Description:    "It doesn't so much attack as simply stop being avoidable.",
		Stats:          Stats{STR: 18, DEX: 14, CON: 22},
		MaxHP:          70,
		AttackStat:     "str",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 9,
	},
	{
		ID: "mire_revenant", Name: "Bog Revenant", Archetype: ArchetypeMire, Tier: TierStandard,
		Description:    "It drowned here once. It would very much like company.",
		Stats:          Stats{STR: 16, DEX: 20, CON: 20},
		MaxHP:          80,
		AttackStat:     "dex",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 10,
		Condition: ConditionPoison, ConditionValue: 40,
	},
	// Note: this list used to end with "mire_tyrant" (Swamp Tyrant),
	// the Stage 10/Part 3 finale. That encounter has been replaced by
	// the simultaneous 3-enemy Thaddeus ambush — see ambush.go and
	// stages.go's Stage 10/Part 3 entry, which now points at
	// AmbushEncounterID instead of a single Enemies-list ID.
}

// GetEnemy mirrors GetClass/GetWeapon/GetArmor's (found, ok) lookup
// pattern — stages.go references enemies by ID, and a typo'd ID there
// should fail loudly at content-authoring time (caught by a future
// test), not panic a live combat request.
//
// Also checks BossPhases, not just Enemies: once ensureInCombat
// (handlers/game.go) spawns a boss phase, InCombat.EnemyID holds that
// phase's ID (e.g. "final_boss_phase1"), and every subsequent lookup
// of the CURRENT enemy — building the attack profile, resolving the
// counterattack, computing enemy AC — goes through GetEnemy the exact
// same way it would for an ordinary Enemies entry (see boss.go's doc
// comment: "every existing per-enemy resolution path treats a boss
// phase exactly like fighting any other single enemy"). Without this,
// GetEnemy would only find the phase on the very first, spawning call
// and fail every call after — which is exactly the "unknown enemy
// final_boss_phase1" bug this comment is here to prevent regressing.
func GetEnemy(id string) (Enemy, bool) {
	for _, e := range Enemies {
		if e.ID == id {
			return e, true
		}
	}
	for _, e := range BossPhases {
		if e.ID == id {
			return e, true
		}
	}
	for _, e := range AmbushEnemies {
		if e.ID == id {
			return e, true
		}
	}
	return Enemy{}, false
}

// DifficultyModifiers is the scaling applied to every enemy for a
// given Difficulty. Centralized as one lookup so combat balance is
// tuned in exactly one place, not scattered across every call site
// that spawns an enemy.
type DifficultyModifiers struct {
	// HPMultiplier scales Enemy.MaxHP.
	HPMultiplier float64
	// StatBonus is added to every one of an enemy's three stats
	// (STR/DEX/CON) — flat, not a percentage, since these values are
	// already small (roughly 7-19) and a percentage would barely move
	// the derived D&D modifier at this scale.
	StatBonus int
	// DamageBonus adds directly to Enemy.DamageBonus.
	DamageBonus int
}

// GetDifficultyModifiers returns the scaling for a Difficulty.
// stages.go's base numbers are tuned as the harder baseline (genre
// convention: the default a designer balances around is the harder
// setting), so Easy softens enemies below base and Hard pushes them
// further above it.
func GetDifficultyModifiers(d Difficulty) DifficultyModifiers {
	switch d {
	case DifficultyEasy:
		return DifficultyModifiers{HPMultiplier: 0.8, StatBonus: -1, DamageBonus: -1}
	case DifficultyHard:
		return DifficultyModifiers{HPMultiplier: 1.3, StatBonus: 2, DamageBonus: 2}
	default:
		return DifficultyModifiers{HPMultiplier: 1.0}
	}
}

// ApplyDifficulty returns a copy of enemy with this difficulty's
// modifiers folded in. Never mutates the package-level Enemies slice
// — same immutability convention as Classes/Weapons/Armor. InCombat
// only persists EnemyID + EnemyCurrentHP (see state.go), not a scaled
// snapshot, so this gets called fresh every time an enemy is read —
// that's what keeps one save file's difficulty consistent across
// every subsequent look at the same enemy.
func ApplyDifficulty(enemy Enemy, difficulty Difficulty) Enemy {
	mods := GetDifficultyModifiers(difficulty)

	enemy.Stats.STR += mods.StatBonus
	enemy.Stats.DEX += mods.StatBonus
	enemy.Stats.CON += mods.StatBonus

	scaledHP := int(float64(enemy.MaxHP) * mods.HPMultiplier)
	if scaledHP < 1 {
		scaledHP = 1
	}
	enemy.MaxHP = scaledHP

	enemy.DamageBonus += mods.DamageBonus

	return enemy
}