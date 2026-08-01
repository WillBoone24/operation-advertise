package game

// AmbushEncounterID is the sentinel value stages.go's Stage 10/Part 3
// Encounter.EnemyID is set to, instead of a real Enemies-list ID —
// mirrors BossEncounterID's role in boss.go exactly. A normal
// encounter has one enemy with one HP bar; this fight has three, all
// alive and swinging at once, so it can't be represented as a single
// Enemies entry the way every other encounter in stages.go is.
// ensureInCombat (handlers/game.go) checks for this exact value to
// decide whether to spawn all of AmbushEnemies at once (into
// InCombat.Enemies) instead of doing an ordinary GetEnemy lookup.
//
// Unlike BossPhases, this is NOT a phase system — all three enemies
// below are live simultaneously from the first round, not one at a
// time. See state.go's InCombat.Enemies doc comment for the resulting
// data-model split.
const AmbushEncounterID = "thaddeus_ambush"

// AmbushEnemies is the Stage 10/Part 3 "band of backstabbers" —
// Thaddeus and two hired blades, ambushing the player one stretch of
// road from the castle. Each is its own Enemy entry (own ID, own HP
// pool, own stats) so every existing per-enemy resolution path
// (ResolveAttack, ApplyDifficulty, BuildEnemyAttackProfile) treats each
// of the three exactly like fighting any other single enemy — only
// the SPAWNING and TURN-STRUCTURE logic in handlers/game.go needs to
// know three of them are alive at once.
//
// IDs are deliberately plain ("thaddeus", not "ambush_thaddeus") so
// the player's targeting command (`attack thaddeus`) matches the ID
// directly — see handlers/game.go's resolveAmbushTarget.
var AmbushEnemies = []Enemy{
	{
		ID: "thaddeus", Name: "Thaddeus the Betrayer", Archetype: ArchetypeMire, Tier: TierElite,
		Description:    "You knew him once — fought beside him, trusted him with your back. He doesn't meet your eyes as he draws his blade.",
		Stats:          Stats{STR: 20, DEX: 16, CON: 20},
		MaxHP:          70,
		AttackStat:     "str",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 8,
		Condition: ConditionBonusCritRange, ConditionValue: 2,
	},
	{
		ID: "alfonse", Name: "Alfonse", Archetype: ArchetypeMire, Tier: TierStandard,
		Description:    "A hired blade, paid well enough not to ask who used to call the man beside him a friend.",
		Stats:          Stats{STR: 16, DEX: 14, CON: 15},
		MaxHP:          45,
		AttackStat:     "str",
		DamageDieSides: 8, DamageDieCount: 1, DamageBonus: 5,
	},
	{
		ID: "aragorn", Name: "Aragorn", Archetype: ArchetypeMire, Tier: TierStandard,
		Description:    "Quick, quiet, and already circling for an opening the moment the ambush breaks cover.",
		Stats:          Stats{STR: 12, DEX: 18, CON: 13},
		MaxHP:          35,
		AttackStat:     "dex",
		DamageDieSides: 6, DamageDieCount: 1, DamageBonus: 4,
		Condition: ConditionOneHitKillChance, ConditionValue: 8,
	},
}

// GetAmbushEnemy looks up one of AmbushEnemies by ID — used by
// GetEnemy's fallback below, and directly by handlers/game.go anywhere
// it needs a specific ambush combatant's base (pre-difficulty)
// definition without pulling in the whole slice.
func GetAmbushEnemy(id string) (Enemy, bool) {
	for _, e := range AmbushEnemies {
		if e.ID == id {
			return e, true
		}
	}
	return Enemy{}, false
}