package game

import (
	"encoding/json"
	"fmt"
)

// Difficulty is the run-wide easy/hard toggle, chosen once at
// character creation. Stored as a string (like ClassID) for the same
// human-readable-save-file reason.
type Difficulty string

const (
	DifficultyEasy Difficulty = "easy"
	DifficultyHard Difficulty = "hard"
)

// Equipped holds a character's currently-equipped weapon and armor IDs
// — always exactly one of each, no empty-slot state, since every class
// starts with a StartingWeaponID (classes.go) and character creation
// (Day 6) will assign a default armor piece too. Modeled as IDs (not
// embedded Item structs) so this stays a thin reference and the actual
// item data (classes.go/items.go's Weapons/Armor maps) remains the
// single source of truth — an equipped item's stats can never drift
// out of sync with its definition because there's only one copy of
// that definition anywhere.
type Equipped struct {
	WeaponID string `json:"weapon_id"`
	ArmorID  string `json:"armor_id"`
}

// InCombat holds the state of an encounter currently in progress, so a
// player who closes the terminal mid-fight resumes the SAME fight
// (same enemy, same remaining enemy HP) on their next login rather
// than the fight silently resetting or vanishing. Nil when not
// currently in combat — combat.go and the future action handler use
// its presence/absence as the source of truth for "is this player
// mid-fight," rather than a separate boolean flag that could
// theoretically disagree with it.
type InCombat struct {
	EnemyID        string `json:"enemy_id"`
	EnemyCurrentHP int    `json:"enemy_current_hp"`

	// PoisonTurns/BurnTurns/StunTurns count down the ENEMY's remaining
	// rounds under each status ailment (items.go's ConditionPoison/
	// Burn/Stun) — 0 means "not currently affected." All three are
	// enemy-side only; there is no player-side ailment tracking, since
	// nothing in this design applies these to the player.
	PoisonTurns int `json:"poison_turns,omitempty"`
	BurnTurns   int `json:"burn_turns,omitempty"`
	StunTurns   int `json:"stun_turns,omitempty"`

	// BossPhase is 0 for a normal (non-boss) fight, or 1/2/3 while
	// fighting one of the three-phase final boss's phases (boss.go).
	// InCombat.EnemyID during a boss fight holds the CURRENT phase's
	// own ID (boss.go's BossPhases entries each have a distinct ID) —
	// BossPhase is what lets handlers/game.go tell "defeated the
	// current phase" apart from "defeated the boss outright" without
	// re-deriving that from EnemyID string-matching.
	BossPhase int `json:"boss_phase,omitempty"`

	// Enemies is non-empty ONLY for a simultaneous multi-enemy fight
	// (currently just the Stage 10/Part 3 ambush — see ambush.go).
	// When it's set, EnemyID/EnemyCurrentHP/PoisonTurns/BurnTurns/
	// StunTurns/BossPhase above are all unused and stay zero-valued —
	// this is a SEPARATE fight shape, not a generalization of the
	// single-enemy one above, and handlers/game.go branches once on
	// "is Enemies non-nil" at the top of every combat-facing function
	// rather than threading a length-1-vs-length-3 case through the
	// existing single-enemy logic. Every other encounter in the game
	// (all 17 ordinary fights plus the 3-phase final boss) leaves this
	// nil and is completely untouched by its existence.
	Enemies []CombatEnemy `json:"enemies,omitempty"`

	// NextAttackGuaranteedHit is set by Ranger's Steady Aim ability
	// (classes.go) and consumed by the very next "attack" action,
	// which skips the normal d20-vs-AC roll entirely — mirrors how
	// Spell.AutoHit already bypasses that same check for Firebolt.
	// Cleared on consumption whether or not the attack was actually
	// lethal, and implicitly cleared any time InCombat itself is
	// cleared/respawned, same as every other per-fight flag here.
	NextAttackGuaranteedHit bool `json:"next_attack_guaranteed_hit,omitempty"`

	// FirstAttackLanded tracks whether the player has landed at
	// least one attack in THIS fight — Rogue's Sneak Attack
	// (classes.go) triggers only on the very first LANDED attack
	// (a miss doesn't burn the window) against an enemy still at
	// full HP. Per-fight, not per-stage — unlike
	// SaveState.AbilityUsedThisStage, this resets every new
	// encounter automatically just by InCombat being recreated.
	FirstAttackLanded bool `json:"first_attack_landed,omitempty"`
}

// CombatEnemy is one living combatant in a simultaneous multi-enemy
// fight (InCombat.Enemies above). Unlike the single-enemy shape, each
// enemy here carries its OWN ailment counters — poison/burn/stun are
// no longer a fight-wide concept once more than one enemy can be
// independently poisoned, burned, or stunned at once.
type CombatEnemy struct {
	ID        string `json:"id"`
	CurrentHP int    `json:"current_hp"`

	PoisonTurns int `json:"poison_turns,omitempty"`
	BurnTurns   int `json:"burn_turns,omitempty"`
	StunTurns   int `json:"stun_turns,omitempty"`
}

// SaveState is the full shape of a character, and is exactly what gets
// JSON-marshaled into the `save_data` TEXT column on the users table
// (see backend/internal/database/migrate.go — that column has existed
// since Phase 1 specifically as an unparsed placeholder for this). No
// other representation of "a character" exists — GameState.js on the
// frontend (Day 9) will mirror this shape, the same DUMB-mirror
// relationship GameState.js already has with the user profile today.
type SaveState struct {
	CharacterName string     `json:"character_name"`
	Class         ClassID    `json:"class"`
	Difficulty    Difficulty `json:"difficulty"`

	CurrentHP int `json:"current_hp"`

	// CurrentStage/CurrentPart point at the NEXT encounter to play,
	// starting at (1,1) for a brand-new character. When these exceed
	// TotalStages/PartsPerStage (stages.go), the run is complete — see
	// IsRunComplete below, which is the only place that comparison
	// should be made, so it can't drift out of sync between callers.
	CurrentStage int `json:"current_stage"`
	CurrentPart  int `json:"current_part"`

	Equipped  Equipped `json:"equipped"`
	Inventory []string `json:"inventory"` // armor + potion IDs held but not equipped/consumed

	// StatBonuses accumulates permanent boosts from consumed stat
	// potions (items.go's PotionKindStat), added on top of class base
	// stats in EffectiveStats below. Kept separate from
	// Equipped-derived bonuses since these persist across gear
	// changes — swapping armor never rolls back a stat potion's
	// effect, the way it would if this were folded into StatMods on
	// some equippable item instead.
	StatBonuses Stats `json:"stat_bonuses,omitempty"`

	// Mana fuels a Mage's permanent spell list (game/spells.go's
	// MageSpells) — spent 1-per-cast (every spell costs exactly 1,
	// see Spell.ManaCost), earned back 1-per-victory (handlers/game.go
	// awards it on any won fight, gated to the Mage class there rather
	// than here, so this field stays a plain int usable — if unused —
	// by every class rather than needing a Mage-only branch on the
	// struct itself). Zero-value on a brand-new character, same as
	// every other earned-not-granted resource in this struct.
	Mana int `json:"mana,omitempty"`

	InCombat *InCombat `json:"in_combat,omitempty"`

	// AbilityUsedThisStage tracks the once-per-stage class abilities
	// (Fighter's Second Wind, Cleric's Mend — see classes.go's
	// AbilityDescription text). Reset to false whenever CurrentStage
	// advances (see AdvancePart below) — "once per stage" is enforced
	// HERE, at the state-transition boundary, not re-derived by
	// whatever handler happens to process an ability-use action.
	AbilityUsedThisStage bool `json:"ability_used_this_stage"`

	// PendingAdvance is true when the player has just cleared an
	// encounter's combat but hasn't yet moved to the next stage/part.
	// Lets POST /api/game/action distinguish "never engaged this
	// encounter" from "won it, waiting on a descend action" — both
	// otherwise look identical (InCombat == nil). Omitted from JSON
	// when false so old save data without this field unmarshals
	// cleanly.
	PendingAdvance bool `json:"pending_advance,omitempty"`

	// MarksOfMadness tracks how many times this character has died during
	// the current run. On the fifth mark, the run collapses.
	MarksOfMadness int `json:"marks_of_madness"`

	// LockedUntil is a Unix timestamp. If it is greater than the current
	// time, the player cannot log into this character.
	LockedUntil int64 `json:"locked_until,omitempty"`

	// --- Tavern fields (see game/tavern.go) ---

	// Gold is the class-agnostic currency earned from combat kills
	// (game.RollGoldDrop, applied in handlers/game.go's
	// grantVictoryRewards) and spent on tavern purchases
	// (handleTavernBuy). Unlike Mana, every class can hold and spend
	// it — there's no class gate on the currency itself, only on what
	// a given purchase (e.g. a Mage-only scroll) requires.
	Gold int `json:"gold,omitempty"`

	// LearnedSpells holds the IDs of any game.ScrollSpells this
	// character has bought in the tavern — permanent, additive to the
	// fixed MageSpells kit, never removed. See game.GetKnownSpell,
	// which is what actually checks this list at cast time.
	LearnedSpells []string `json:"learned_spells,omitempty"`

	// HasLearnedMonsterLore is set once the player pays to learn
	// spell-effectiveness lore in the tavern (handleTavernLore). A
	// flat, one-time unlock (no re-purchase, no per-archetype partial
	// state) — once known, game.MonsterLore is included in every
	// subsequent state response (see gameStateResponse.MonsterLore).
	HasLearnedMonsterLore bool `json:"has_learned_monster_lore,omitempty"`

	// AtTavern is true whenever the character is currently standing in
	// the tavern rather than the dungeon. Set true automatically the
	// moment Stage 2's finale is cleared (handleDescend) and permanently
	// once IsRunComplete() (handleDescend again) — permanently, because
	// the tavern is meant to be the only place a finished run can still
	// act from. Combat actions (handleAttack/handleCast/handleDescend)
	// all reject while this is true; only handleTavernLeave can clear
	// it, and it refuses to if the run is already complete.
	AtTavern bool `json:"at_tavern,omitempty"`

	// TavernRiddleSolved caps the riddle's +3 mana reward to once per
	// run, regardless of how many times the tavern is re-entered or
	// the riddle re-rolled — see handleTavernRiddle.
	TavernRiddleSolved bool `json:"tavern_riddle_solved,omitempty"`

	// ShamanBlessingReceived caps the Stage 2 finale's one-time +5/+5/+5
	// stat blessing to a single application per run — mirrors
	// TavernRiddleSolved's guard above for the same reason: the
	// triggering code path should only ever fire once per run anyway,
	// but this flag makes that a guarantee instead of an assumption.
	ShamanBlessingReceived bool `json:"shaman_blessing_received,omitempty"`

	// CurrentRiddleID is the ID (game.TavernRiddle.ID) of the riddle
	// most recently handed to this player, so a later answer-submission
	// request (a separate, stateless HTTP call) can look up which
	// riddle it's actually being checked against. Cleared back to ""
	// once solved correctly; a wrong answer leaves it in place so the
	// player can keep trying the SAME riddle rather than silently
	// getting a new one on every failed guess.
	CurrentRiddleID string `json:"current_riddle_id,omitempty"`

	// CurrentTavernSpells holds the game.ScrollSpells IDs (always
	// game.TavernScrollOfferCount of them) currently on offer in the
	// tavern, drawn from the full 7-spell pool by
	// game.RollTavernSpells. Rolled fresh exactly once per tavern
	// VISIT — handlers/game.go's handleDescend sets this the same
	// moment it sets AtTavern true — and left untouched for the rest
	// of that visit, so re-opening the tavern menu (or any other
	// stateless request in between) keeps showing the same two spells
	// instead of re-rolling on every look. The next visit (this run's
	// other AtTavern waypoint, or a future run) rolls again.
	CurrentTavernSpells []string `json:"current_tavern_spells,omitempty"`

	// BlackjackRoundsPlayed and RouletteRoundsPlayed each cap their
	// game to game.BlackjackMaxRounds/game.RouletteMaxRounds rounds per
	// RUN (not per tavern visit — see BlackjackMaxRounds's doc comment)
	// — incremented once per completed round/spin in handlers/game.go's
	// finishBlackjackRound and handleTavernRoulette respectively, and
	// reset to 0 alongside every other run-scoped tavern field below.
	BlackjackRoundsPlayed int `json:"blackjack_rounds_played,omitempty"`
	RouletteRoundsPlayed  int `json:"roulette_rounds_played,omitempty"`

	// BlackjackActive is true exactly while one blackjack round is
	// mid-hand — dealt, but not yet resolved by a bust or a stand. A
	// stateless HTTP call can't "remember" a hand between the deal and
	// the next hit/stand the way an in-process game loop could, so the
	// hand itself has to round-trip through SaveState the same way
	// CurrentRiddleID lets a riddle round-trip between being asked and
	// being answered. Only handleTavernLeave (handlers/game.go) checks
	// this to refuse leaving mid-hand; every blackjack sub-action
	// (start/hit/stand) checks it directly.
	BlackjackActive bool `json:"blackjack_active,omitempty"`

	// BlackjackWager is the gold already risked on the CURRENT round
	// (set once, at deal time, from handleTavernBlackjack's Amount —
	// see actionRequest.Amount) — never re-read from the request on
	// hit/stand, so a player can't change their wager mid-hand.
	BlackjackWager int `json:"blackjack_wager,omitempty"`

	// BlackjackPlayerCards and BlackjackDealerCards hold the ranks
	// (game.DrawBlackjackCard's return values, e.g. "A"/"10"/"K") drawn
	// so far this round, in draw order. Both are nil whenever
	// BlackjackActive is false. See blackjack.go's doc comment on why
	// these are ranks from an infinite shoe rather than indices into a
	// depleting deck.
	BlackjackPlayerCards []string `json:"blackjack_player_cards,omitempty"`
	BlackjackDealerCards []string `json:"blackjack_dealer_cards,omitempty"`

	// Familiar holds the ID (familiars.go's Familiars map) of the
	// character's currently-bonded familiar, "" if none. This is the
	// PRIMARY familiar — earned via the ordinary random combat drop
	// (familiars.go's RollFamiliarDrop), Stage 4+. See its doc
	// comment on why a random drop is fully suppressed while this is
	// already non-empty.
	Familiar string `json:"familiar,omitempty"`

	// SecondFamiliar holds the ID of a second, guaranteed companion
	// granted on clearing the Journey's Ancient Woods finale (Stage
	// 8/Part 3 — see stages.go's AncientWoodsFamiliarStage/Part and
	// familiars.go's GrantSecondFamiliar). Unlike Familiar, this one
	// is never randomly rolled — it's a fixed story beat, so it gets
	// its own field rather than overloading Familiar with "the second
	// one that arrived a different way." Both fight every combat
	// round when present — see handlers/game.go's
	// resolveFamiliarMiniTurn, which now loops over every non-empty
	// familiar slot instead of assuming exactly one.
	SecondFamiliar string `json:"second_familiar,omitempty"`

	// DungeonComplete is set true the moment Stage 5's finale (the
	// final boss) is cleared — see handlers/game.go's handleDescend.
	// Distinct from IsRunComplete(), which now only fires once the
	// Journey (Stages 6-10, stages.go) is ALSO finished: this flag is
	// what actually gates `tavern exit` (handleExitDungeon), the
	// one-time waypoint between "beat the dungeon" and "begin the
	// journey home."
	DungeonComplete bool `json:"dungeon_complete,omitempty"`

	// AtKingsChambers is set true (instead of AtTavern) the moment
	// IsRunComplete() first becomes true — i.e. the Journey's Black
	// Mire finale is cleared (see handlers/game.go's handleDescend).
	// This is the true end of a run: not another tavern, but a
	// one-time audience with the king, gated behind its own actions
	// (handleLegacyHall/handleChoosePath) the same way AtTavern gates
	// tavern_* — see game/legacy.go's package doc comment for the
	// full shape of what happens here.
	AtKingsChambers bool `json:"at_kings_chambers,omitempty"`

	// LegacyPath holds the game.LegacyPathID this character chose in
	// the king's chambers, "" until chosen. Once set, it never
	// changes again — handleChoosePath refuses a second call, and
	// this field is what that refusal checks. Also what's persisted
	// (via database.InsertLegacy) into the shared, cross-character
	// Hall of Legacies every future completed run gets shown.
	LegacyPath string `json:"legacy_path,omitempty"`
}

// NewSaveState creates a brand-new character: full HP for the chosen
// class, starting weapon from the class definition, no armor equipped
// yet (see the design note below on why that's correct, not an
// oversight), standing at Stage 1/Part 1.
//
// Returns an error rather than a zero-value SaveState if classID is
// unrecognized, since the caller (the future POST /api/game/create
// handler) receives classID from client input and needs a clean way
// to reject "unknown class" as a 400, not silently create a broken
// character.

func NewSaveState(characterName string, classID ClassID, difficulty Difficulty) (SaveState, error) {
	class, ok := GetClass(classID)
	if !ok {
		return SaveState{}, fmt.Errorf("game: unknown class %q", classID)
	}

	if difficulty != DifficultyEasy && difficulty != DifficultyHard {
		return SaveState{}, fmt.Errorf("game: unknown difficulty %q", difficulty)
	}

	return SaveState{
		CharacterName: characterName,
		Class:         classID,
		Difficulty:    difficulty,
		CurrentHP:     class.BaseMaxHP,
		CurrentStage:  1,
		CurrentPart:   1,
		Equipped: Equipped{
			WeaponID: class.StartingWeaponID,
			// ArmorID intentionally starts empty. Every stage finale
			// grants exactly one armor piece (stages.go's
			// RewardArmorID), so "no armor yet" is the correct, honest
			// starting state — not a bug to paper over with a fake
			// default armor item that isn't actually one of the 5
			// defined pieces.
			ArmorID: "",
		},
		Inventory:            []string{},
		AbilityUsedThisStage: false,
	}, nil
}

// EffectiveStats returns this character's Stats after adding permanent
// stat-potion bonuses and equipped weapon/armor bonuses on top of
// their class base stats. This is the ONLY function that should ever
// be used to determine a character's combat-relevant stats — never
// read class.BaseStats directly once a character exists, or a
// potion's/equipped item's bonus silently gets ignored.
func (s SaveState) EffectiveStats() (Stats, error) {
	class, ok := GetClass(s.Class)
	if !ok {
		return Stats{}, fmt.Errorf("game: save state has unknown class %q", s.Class)
	}

	stats := addStats(class.BaseStats, s.StatBonuses)

	if s.Equipped.WeaponID != "" {
		weapon, ok := GetWeapon(s.Equipped.WeaponID)
		if !ok {
			return Stats{}, fmt.Errorf("game: save state has unknown weapon %q", s.Equipped.WeaponID)
		}
		stats = addStats(stats, weapon.StatMods)
	}

	if s.Equipped.ArmorID != "" {
		armor, ok := GetArmor(s.Equipped.ArmorID)
		if !ok {
			return Stats{}, fmt.Errorf("game: save state has unknown armor %q", s.Equipped.ArmorID)
		}
		stats = addStats(stats, armor.StatMods)
	}

	return stats, nil
}

// EffectiveMaxHP mirrors EffectiveStats but for max HP: class base +
// any equipped armor's HPMod. Weapons never modify HP, so only armor
// is consulted here.
func (s SaveState) EffectiveMaxHP() (int, error) {
	class, ok := GetClass(s.Class)
	if !ok {
		return 0, fmt.Errorf("game: save state has unknown class %q", s.Class)
	}

	maxHP := class.BaseMaxHP

	if s.Equipped.ArmorID != "" {
		armor, ok := GetArmor(s.Equipped.ArmorID)
		if !ok {
			return 0, fmt.Errorf("game: save state has unknown armor %q", s.Equipped.ArmorID)
		}
		maxHP += armor.HPMod
	}

	return maxHP, nil
}

// IsRunComplete reports whether this character has cleared every
// stage. The sole authority on this question — see CurrentStage's doc
// comment above on why nothing else should re-derive it independently.
func (s SaveState) IsRunComplete() bool {
	return s.CurrentStage > TotalStages
}

// AdvancePart moves CurrentStage/CurrentPart forward by one encounter,
// rolling over into the next stage (and resetting
// AbilityUsedThisStage) when a stage's final part is cleared. Called
// by the future action handler after a combat encounter is won —
// combat.go itself never mutates SaveState directly, keeping "did we
// win" (combat.go's job) and "what happens to progression as a result"
// (this function's job) as separate concerns.
func (s *SaveState) AdvancePart() {
	if s.CurrentPart < PartsPerStage {
		s.CurrentPart++
		return
	}

	s.CurrentStage++
	s.CurrentPart = 1
	s.AbilityUsedThisStage = false
}

// addStats is a small unexported helper — Stats has no arithmetic
// methods of its own (see classes.go) since addition is only ever
// needed here, in exactly this one place, rather than being a
// general-purpose operation every caller of Stats needs to reach for.

// ResetRun wipes all run progress after the player reaches five
// Marks of Madness. The account remains, but the run starts over.
func (s *SaveState) ResetRun() error {
	class, ok := GetClass(s.Class)
	if !ok {
		return fmt.Errorf("game: unknown class %q", s.Class)
	}

	s.CurrentStage = 1
	s.CurrentPart = 1

	s.Inventory = []string{}

	s.Equipped = Equipped{
		WeaponID: class.StartingWeaponID,
		ArmorID:  "",
	}

	s.StatBonuses = Stats{}
	s.Mana = 0

	s.InCombat = nil
	s.PendingAdvance = false
	s.AbilityUsedThisStage = false

	s.CurrentHP = class.BaseMaxHP

	s.MarksOfMadness = 0

	// Tavern progress resets along with everything else — this mirrors
	// Mana/StatBonuses/Inventory above being wiped rather than
	// preserved, since ResetRun models "the run collapses, start over"
	// rather than "keep your out-of-run knowledge." Gold, learned
	// scrolls, and the lore unlock are all run-scoped rewards, same
	// category as the Mana/Inventory this function already clears.
	s.Gold = 0
	s.LearnedSpells = nil
	s.HasLearnedMonsterLore = false
	s.AtTavern = false
	s.TavernRiddleSolved = false
	s.ShamanBlessingReceived = false
	s.CurrentRiddleID = ""
	s.CurrentTavernSpells = nil
	s.BlackjackRoundsPlayed = 0
	s.RouletteRoundsPlayed = 0
	s.BlackjackActive = false
	s.BlackjackWager = 0
	s.BlackjackPlayerCards = nil
	s.BlackjackDealerCards = nil
	s.Familiar = ""
	s.SecondFamiliar = ""
	s.DungeonComplete = false
	s.AtKingsChambers = false
	s.LegacyPath = ""

	return nil
}

func addStats(a, b Stats) Stats {
	return Stats{
		STR: a.STR + b.STR,
		DEX: a.DEX + b.DEX,
		CON: a.CON + b.CON,
	}
}

// MarshalSaveState and UnmarshalSaveState are the only functions that
// should touch encoding/json for SaveState — keeping the
// serialization boundary explicit and in one place (mirroring
// database/users.go being "the only file allowed to write SQL")
// rather than letting handlers call json.Marshal/Unmarshal on
// SaveState directly, scattered across call sites.

// MarshalSaveState serializes a SaveState to the string form stored in
// the users.save_data column.
func MarshalSaveState(s SaveState) (string, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return "", fmt.Errorf("game: marshal save state: %w", err)
	}
	return string(b), nil
}

// UnmarshalSaveState parses the users.save_data column back into a
// SaveState. An empty string (a brand-new account that has never
// created a character — see database/users.go's CreateUser, which
// inserts save_data as ”) is NOT an error here; it's a normal,
// expected state meaning "no character yet," reported via the second
// return value so callers (the future GET /api/game/state handler)
// can return a clean "no character" response instead of a parse
// failure.
func UnmarshalSaveState(raw string) (state SaveState, hasCharacter bool, err error) {
	if raw == "" {
		return SaveState{}, false, nil
	}

	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return SaveState{}, false, fmt.Errorf("game: unmarshal save state: %w", err)
	}

	return state, true, nil
}