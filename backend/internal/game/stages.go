package game

// Encounter is one of the 15 hand-authored beats a character plays
// through (5 stages × 3 parts). No procedural generation — see the
// project's scope decision to cut the original "100 floors" idea down
// to a small, hand-authored run instead.
type Encounter struct {
	Stage int `json:"stage"` // 1-5
	Part  int `json:"part"`  // 1-3

	Description string `json:"description"`
	EnemyID     string `json:"enemy_id"`

	// IsStageFinale marks part 3 of every stage — the harder encounter
	// that also grants the stage's armor reward (see RewardArmorID).
	// Kept as an explicit bool rather than inferring "Part == 3"
	// everywhere it matters, so a future change to parts-per-stage
	// doesn't silently break finale detection at every call site.
	IsStageFinale bool `json:"is_stage_finale"`

	// RewardArmorID is the Armor (items.go) ID granted on clearing
	// this encounter, empty if none. Deliberately ARMOR-only, never a
	// class-restricted Weapon — a static encounter can't know which
	// class is playing it, and handing a Fighter a wand they can never
	// equip is a worse reward than no reward. Armor has no such
	// restriction (see items.go's design note on that asymmetry),
	// which is exactly why it's the only lootable/rewardable pool for
	// now. Weapon variety beyond the class starting weapon is
	// explicitly out of scope for this pass.
	RewardArmorID string `json:"reward_armor_id,omitempty"`

	// RewardPotionID is a Potion (items.go) ID granted alongside the
	// armor reward on clearing this encounter, empty if none. Same
	// finale-only distribution as RewardArmorID — potions have no
	// separate drop table, they just ride along with the existing
	// stage-clear reward moment. Alternates stat/hp across the 5
	// finales below purely for variety; adjust freely per-stage if a
	// different pacing is wanted later.
	RewardPotionID string `json:"reward_potion_id,omitempty"`
}

// Stages is the fixed list of all 15 encounters, ordered stage 1→5,
// part 1→3 within each. Enemy tier escalates roughly with stage
// number (see the tier column below) so difficulty ramps across the
// whole run rather than jumping straight to Elite-tier threats.
//
// Difficulty tier per part, by stage:
//
//	Stage 1: Lesser, Lesser, Standard   (finale)
//	Stage 2: Lesser, Standard, Standard (finale)
//	Stage 3: Standard, Standard, Elite   (finale)
//	Stage 4: Standard, Elite, Elite      (finale)
//	Stage 5: Elite, Elite, Elite         (finale — final boss)
//
// Armor rewards are handed out one per stage finale, in ascending
// order of the piece's overall power (see items.go's Armor map),
// so gear progression tracks stage progression.
var Stages = []Encounter{
	// --- Stage 1 ---
	{Stage: 1, Part: 1, EnemyID: "brute_lesser",
		Description: "The dungeon entrance narrows into a low tunnel. Something is waiting in the dark ahead."},
	{Stage: 1, Part: 2, EnemyID: "stalker_lesser",
		Description: "A side passage, quiet for too long — quiet enough that you hear it before you see it."},
	{Stage: 1, Part: 3, EnemyID: "caster_standard", IsStageFinale: true, RewardArmorID: "a_leather", RewardPotionID: "p_hp_elixir",
		Description: "The tunnel opens into a torchlit chamber. A conjurer turns to face you, already mid-cast."},

	// --- Stage 2 ---
	{Stage: 2, Part: 1, EnemyID: "stalker_lesser",
		Description: "Loose gravel underfoot — every step announces you. It's already circling."},
	{Stage: 2, Part: 2, EnemyID: "brute_standard",
		Description: "A collapsed archway blocks the only way forward. Something is clearing it from the other side."},
	{Stage: 2, Part: 3, EnemyID: "stalker_standard", IsStageFinale: true, RewardArmorID: "a_robe", RewardPotionID: "p_stat_tonic",
		Description: "A ring of old runes, still faintly glowing. Whatever guards it moves faster than the light does."},

	// --- Stage 3 ---
	{Stage: 3, Part: 1, EnemyID: "brute_standard",
		Description: "The air turns cold. Frost creeps up the walls ahead of something large."},
	{Stage: 3, Part: 2, EnemyID: "caster_standard",
		Description: "A flooded chamber, knee-deep water reflecting torchlight that isn't yours."},
	{Stage: 3, Part: 3, EnemyID: "brute_elite", IsStageFinale: true, RewardArmorID: "a_cloak", RewardPotionID: "p_hp_elixir",
		Description: "A warlord's chamber, trophies of past intruders lining the walls. It's been waiting for a new one."},

	// --- Stage 4 ---
	{Stage: 4, Part: 1, EnemyID: "stalker_standard",
		Description: "A maze of narrow corridors. It knows this maze better than you ever will."},
	{Stage: 4, Part: 2, EnemyID: "caster_elite",
		Description: "A chamber lit only by the caster's own conjured light — and it doesn't want company."},
	{Stage: 4, Part: 3, EnemyID: "stalker_elite", IsStageFinale: true, RewardArmorID: "a_chainmail", RewardPotionID: "p_stat_tonic",
		Description: "Total darkness, but you're being watched. You just don't know from where yet."},

	// --- Stage 5 (final) ---
	{Stage: 5, Part: 1, EnemyID: "brute_elite",
		Description: "The final descent. The walls here are scored with claw marks from things that didn't make it out."},
	{Stage: 5, Part: 2, EnemyID: "caster_elite",
		Description: "A ritual chamber, mid-ritual. Interrupting it seems like the only option left."},
	{Stage: 5, Part: 3, EnemyID: BossEncounterID, IsStageFinale: true, RewardArmorID: "a_plate", RewardPotionID: "p_hp_elixir",
		Description: "The dungeon's heart. Whatever has been shadowing your entire run is finally done waiting — and it hasn't finished growing."},

	// -------------------------------------------------------------
	// The Journey (Part 2, Stages 6-10) — reached only after clearing
	// the dungeon's Stage 5 finale (the final boss). Modeled as MORE
	// stages in the same Stage/Part shape rather than a parallel
	// system: every existing mechanic (ensureInCombat, ResolveAttack,
	// ApplyDifficulty, rewards, familiars, the tavern) already just
	// operates on "whatever GetEncounter(stage, part) returns," so
	// five more stages is the entire integration cost.
	//
	// handlers/game.go's handleDescend treats clearing Stage 5/Part 3
	// as its own one-time waypoint (mirroring the existing Stage
	// 2->3 tavern break): it routes the player back to the tavern and
	// sets SaveState.DungeonComplete, but does NOT end the run —
	// IsRunComplete() only fires once Stage 10/Part 3 is cleared too.
	// `tavern exit` (handleExitDungeon) is the only way out of that
	// waypoint, and refuses until DungeonComplete is true.
	//
	// Tonal arc: hopeful countryside (Greenwood Trail) sliding into
	// hostile, oppressive territory (Black Mire) as the player nears
	// home. The second familiar (AncientWoodsFamiliarStage/Part)
	// lands at the emotional midpoint, right before the tone turns.
	// -------------------------------------------------------------

	// --- Stage 6: Greenwood Trail — hopeful, peaceful. The first
	// steps out of the dungeon and into open country. ---
	{Stage: 6, Part: 1, EnemyID: "forest_scout",
		Description: "Sunlight filters through towering oaks. Old stone roads lead onward, and for the first time in a long while, the air smells like something other than the dungeon."},
	{Stage: 6, Part: 2, EnemyID: "forest_hunter",
		Description: "Wildflowers crowd the roadside. Something is pacing you just out of sight, testing whether you've noticed yet."},
	{Stage: 6, Part: 3, EnemyID: "forest_warden", IsStageFinale: true, RewardArmorID: "a_woodland_garb", RewardPotionID: "p_hp_elixir",
		Description: "The trees open onto an old stone marker, moss-grown and half-sunk. Whatever is meant to guard it has already noticed you."},

	// --- Stage 7: Rolling Plains — adventure. The wilderness opens
	// up; still relatively safe, but organized enemies appear. ---
	{Stage: 7, Part: 1, EnemyID: "plains_raider",
		Description: "Golden grassland stretches to the horizon, broken by windmills and the odd abandoned farm. Someone's been picking off travelers on this stretch of road."},
	{Stage: 7, Part: 2, EnemyID: "plains_marauder",
		Description: "An overturned wagon, wheels still spinning. Hoofbeats are closing fast from the tall grass."},
	{Stage: 7, Part: 3, EnemyID: "plains_captain", IsStageFinale: true, RewardArmorID: "a_windswept_mail", RewardPotionID: "p_stat_tonic",
		Description: "A raider camp built into the ruin of an old windmill. Its captain steps out to meet you personally."},

	// --- Stage 8: Ancient Woods — mysterious. The last truly
	// beautiful place, and the emotional midpoint before the world
	// turns darker. Clearing this finale grants a second familiar
	// (see AncientWoodsFamiliarStage/Part below). ---
	{Stage: 8, Part: 1, EnemyID: "woods_sprite",
		Description: "Fog curls around impossibly old trees whose roots have swallowed forgotten shrines. Every step feels watched by something patient."},
	{Stage: 8, Part: 2, EnemyID: "woods_dryad",
		Description: "Moss-covered statues line a road no one has walked in years — except, apparently, whatever just moved between them."},
	{Stage: 8, Part: 3, EnemyID: "woods_guardian", IsStageFinale: true, RewardArmorID: "a_ancient_ward", RewardPotionID: "p_hp_elixir",
		Description: "The forest itself seems to be holding its breath. Something ancient steps out to decide whether you're worthy of passing."},

	// --- Stage 9: Shadow Valley — oppressive. The difficulty wall:
	// sheer cliffs blocking the sun, ruined fortifications, enemies
	// that hit noticeably harder than anything before. ---
	{Stage: 9, Part: 1, EnemyID: "valley_stalker",
		Description: "Sheer cliffs choke out the sunlight. Thick mist clings to the ground, and something in it is already circling."},
	{Stage: 9, Part: 2, EnemyID: "valley_beast",
		Description: "Dead trees claw at a sky you can no longer see. A stream runs dark with old mineral stains — and something bigger than the mist."},
	{Stage: 9, Part: 3, EnemyID: "valley_knight", IsStageFinale: true, RewardArmorID: "a_valley_plate", RewardPotionID: "p_stat_tonic",
		Description: "A ruined fortification from some ancient war. Its last defender never stopped standing post."},

	// --- Stage 10 (final region): Black Mire — despair. Endgame
	// survival; the strongest non-boss enemies in the game. Clearing
	// this finale completes the run — see IsRunComplete and
	// handleDescend's kingdom-arrival narrative. ---
	{Stage: 10, Part: 1, EnemyID: "mire_horror",
		Description: "Endless swamp water beneath a thick green fog. Rotting walkways creak underfoot, and something below the surface just noticed you crossing."},
	{Stage: 10, Part: 2, EnemyID: "mire_revenant",
		Description: "Strange lights drift above the marsh. A half-sunken ruin, and something that drowned here long ago rising to greet you."},
	{Stage: 10, Part: 3, EnemyID: AmbushEncounterID, IsStageFinale: true, RewardArmorID: "a_mire_shroud", RewardPotionID: "p_hp_elixir",
		Description: "The mire is behind you. Ahead, the road firms up, the fog thins, and — for one unguarded moment — it feels like the journey is already over. Then a familiar voice calls your name. Thaddeus steps out from the treeline, two hired blades flanking him, blade already drawn. You fought beside him once. He doesn't explain himself, and you don't get the chance to ask."},
}

// GetEncounter looks up a single Stage+Part combination. Returns
// (Encounter{}, false) for anything out of range — the handler layer
// (Day 6-7) uses that to detect "player has cleared the whole run"
// (asking for a stage/part past the end) versus a genuine bad request.
func GetEncounter(stage, part int) (Encounter, bool) {
	for _, e := range Stages {
		if e.Stage == stage && e.Part == part {
			return e, true
		}
	}
	return Encounter{}, false
}

// TotalStages and PartsPerStage are named constants rather than magic
// numbers scattered across handlers/state.go — e.g. "has the player
// beaten the game" is CurrentStage > TotalStages, not a bare 5.
//
// TotalStages is 10, not 5: Stages 1-5 are the dungeon, Stages 6-10
// are the Journey (Part 2, above) — see DungeonFinaleStage/Part for
// the boundary between them.
const (
	TotalStages   = 10
	PartsPerStage = 3
)

// AncientWoodsFamiliarStage/Part identify the Stage 8/Part 3 (Ancient
// Woods finale) encounter as the guaranteed second-familiar moment —
// see familiars.go's GrantSecondFamiliar and handlers/game.go's
// handleDescend, which checks CurrentStage/CurrentPart against these
// two constants (captured BEFORE AdvancePart, same "where the player
// WAS, not where they're headed" rule handleDescend's existing
// clearingStage2Finale check already follows) rather than a bare
// (8, 3) literal, so a future stage-count change can't silently
// desync the trigger from the stage it's named after.
const (
	AncientWoodsFamiliarStage = 8
	AncientWoodsFamiliarPart  = 3
)

// DungeonFinaleStage/Part identify Stage 5/Part 3 (the final boss) as
// the one-time waypoint that routes the player back to the tavern
// with SaveState.DungeonComplete set, ahead of the Journey (Stage 6+)
// — see handlers/game.go's handleDescend and handleExitDungeon.
const (
	DungeonFinaleStage = 5
	DungeonFinalePart  = 3
)