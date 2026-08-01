package game

// BossEncounterID is the sentinel value stages.go's Stage 5/Part 3
// Encounter.EnemyID is set to, instead of a real Enemies-list ID. A
// normal encounter has one enemy with one HP bar; this fight has
// three (one per entry in BossPhases below), so it can't be
// represented as a single Enemies entry the way every other
// encounter in stages.go is. ensureInCombat (handlers/game.go) checks
// for this exact value to decide whether to spawn BossPhases[0]
// instead of doing an ordinary GetEnemy lookup.
const BossEncounterID = "final_boss"

// BossPhases is the final boss's three-phase fight. InCombat.BossPhase
// (state.go) tracks which phase (1/2/3) is currently active; each
// phase is its own Enemy entry (own ID, own HP pool, own stats) so
// every existing per-enemy resolution path (ResolveAttack,
// ApplyDifficulty, BuildEnemyAttackProfile, resolveEnemyCounterattack)
// treats a boss phase exactly like fighting any other single enemy.
//
// Stat/damage numbers below are a first draft, intentionally pushed
// above brute_elite/stalker_elite/caster_elite (the previous ceiling)
// since this is the run's climax — tune during playtesting same as
// the rest of Enemies.
//
// NOTE: nothing yet advances InCombat.BossPhase or re-spawns the next
// phase on a phase kill — grantVictoryRewards (handlers/game.go) ends
// combat and sets PendingAdvance unconditionally on any kill,
// including a boss phase kill. That phase-transition logic still
// needs to be written before phases 2 and 3 are actually reachable.
var BossPhases = []Enemy{
	// Phase 1: "Menacing Egg" — Passive, so it never counterattacks.
	// Low threat by design; the danger in Phase 1 is meant to come from
	// whatever Phase 2/3 escalate into, not from this opening exchange.
	{
		ID: "final_boss_phase1", Name: "Menacing Egg", Archetype: ArchetypeCaster, Tier: TierElite,
		Description:    "It shouldn't be able to watch you. It's watching you anyway.",
		Stats:          Stats{STR: 8, DEX: 10, CON: 20},
		MaxHP:          30,
		AttackStat:     "dex",
		DamageDieSides: 4, DamageDieCount: 1, DamageBonus: 0,
		Passive: true,
	},
	// Phase 2: the shell cracks. Active counterattacks begin here.
	{
		ID: "final_boss_phase2", Name: "The Unshelled", Archetype: ArchetypeBrute, Tier: TierElite,
		Description:    "What climbs out is not finished growing, and it resents you for interrupting that.",
		Stats:          Stats{STR: 20, DEX: 12, CON: 18},
		MaxHP:          42,
		AttackStat:     "str",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 4,
		Condition: ConditionBonusCritRange, ConditionValue: 3,
	},
	// Phase 3: final form.
	{
		ID: "final_boss_phase3", Name: "The Dungeon's Heart", Archetype: ArchetypeCaster, Tier: TierElite,
		Description:    "There is no more shell left to hide behind, and it no longer wants one.",
		Stats:          Stats{STR: 14, DEX: 20, CON: 16},
		MaxHP:          50,
		AttackStat:     "dex",
		DamageDieSides: 10, DamageDieCount: 1, DamageBonus: 5,
		Condition: ConditionLifesteal, ConditionValue: 25,
	},
}