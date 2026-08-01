package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"

	"operation-advertise/backend/internal/auth"
	"operation-advertise/backend/internal/database"
	"operation-advertise/backend/internal/game"
)

// GameHandler bundles the dependencies needed to serve game routes.
// Kept separate from AuthHandler/ProfileHandler for the same reason
// ProfileHandler is separate from AuthHandler: game routes require an
// already-established identity (via auth.Middleware) and have no
// business issuing tokens.
type GameHandler struct {
	DB *database.DB
}

// NewGameHandler constructs a GameHandler.
func NewGameHandler(db *database.DB) *GameHandler {
	return &GameHandler{DB: db}
}

// ---------------------------------------------------------------------
// Request / response payloads
// ---------------------------------------------------------------------

// createCharacterRequest is the expected JSON body for
// POST /api/game/create.
type createCharacterRequest struct {
	CharacterName string `json:"character_name"`
	Class         string `json:"class"`
	Difficulty    string `json:"difficulty"`
}

// actionRequest is the expected JSON body for POST /api/game/action.
// ItemID is used by "equip" (an armor ID) and "use" (a potion ID);
// SpellID is used only by "cast". Both are ignored by whichever
// actions don't need them — one request shape for all five actions,
// rather than five separate endpoints, since they all mutate the same
// SaveState and share the same auth/load/save boilerplate.
type actionRequest struct {
	Action string `json:"action"` // "attack" | "equip" | "use" | "cast" | "descend" | "tavern_lore" | "tavern_buy" | "tavern_riddle" | "tavern_blackjack" | "tavern_roulette" | "tavern_leave" | "exit_dungeon" | "legacy_hall" | "choose_path"
	ItemID string `json:"item_id,omitempty"`
	// ItemID doubles as the tavern purchase ID for "tavern_buy" (a
	// potion ID from game.TavernPotions or a scroll ID from
	// game.ScrollPrices), the chosen path ID for "choose_path" (one of
	// game.LegacyPaths' IDs), the in-hand sub-action for
	// "tavern_blackjack" ("" to start a fresh round, "hit", or
	// "stand"), and the bet spec for "tavern_roulette" ("red", "black",
	// "odd", "even", or a straight-up pocket number "0"-"36") — same
	// "reuse the field that's already there for the closest-matching
	// existing action" convention SpellID follows for "cast".
	SpellID string `json:"spell_id,omitempty"`
	// Answer is used only by "tavern_riddle" — submitting a guess
	// against SaveState.CurrentRiddleID. Omitted (empty) on the FIRST
	// "tavern_riddle" call of a visit, which is what tells
	// handleTavernRiddle to hand out a fresh riddle rather than check
	// an answer against one.
	Answer string `json:"answer,omitempty"`

	// TargetID is used only by "attack"/"cast" during the Stage
	// 10/Part 3 simultaneous ambush (game.AmbushEncounterID) — one of
	// the living game.AmbushEnemies' IDs ("thaddeus"/"alfonse"/
	// "aragorn"), e.g. `attack thaddeus`. Ignored by every other
	// fight, which has exactly one enemy and nothing to target between.
	// A dedicated field rather than overloading ItemID a fourth way —
	// ItemID already means five different things depending on Action;
	// a targeting concept this central to a whole encounter deserves
	// its own name instead of stretching that further.
	TargetID string `json:"target_id,omitempty"`

	// Amount is the gold wager for "tavern_blackjack" (only meaningful
	// when starting a fresh round — ItemID is "" or omitted; ignored
	// for "hit"/"stand", which reuse the wager already locked in on
	// SaveState.BlackjackWager) and for "tavern_roulette" (every spin).
	// A dedicated numeric field rather than ItemID, since ItemID is a
	// string ID/spec lookup everywhere else it's used and a wager is a
	// plain quantity, not an ID into any table.
	Amount int `json:"amount,omitempty"`
}

// gameStateResponse is the public shape of a character returned by
// create/state/action. It mirrors SaveState but adds a few read-only,
// server-computed fields (MaxHP, Encounter) so the frontend never has
// to re-derive game rules itself — matching the project's existing
// trust model where the client only ever renders what the server
// already decided.
type gameStateResponse struct {
	CharacterName  string             `json:"character_name"`
	Class          string             `json:"class"`
	Difficulty     string             `json:"difficulty"`
	CurrentHP      int                `json:"current_hp"`
	MaxHP          int                `json:"max_hp"`
	Stats          statsResponse      `json:"stats"`
	CurrentStage   int                `json:"current_stage"`
	CurrentPart    int                `json:"current_part"`
	Equipped       equippedResponse   `json:"equipped"`
	Inventory      []string           `json:"inventory"`
	InCombat       *inCombatResponse  `json:"in_combat,omitempty"`
	PendingAdvance bool               `json:"pending_advance"`
	RunComplete    bool               `json:"run_complete"`
	Encounter      *encounterResponse `json:"encounter,omitempty"`

	// Mana and Spells are only ever populated for a Mage character
	// (see buildStateResponse) — every other class has no spell list
	// to cast against, so there's nothing meaningful to show. Spells
	// mirrors game.MageSpells' display fields rather than leaking the
	// game.Spell type directly onto the wire, same convention as
	// equippedResponse/inCombatResponse below.
	Mana   int             `json:"mana,omitempty"`
	Spells []spellResponse `json:"spells,omitempty"`

	// DeathMarks and LockedUntil surface the 5-mark death cycle (see
	// database.RecordDeath) to the frontend so it's always visible,
	// not just at the moment a lockout triggers — statusLine.js
	// prints DeathMarks alongside HP/stats on every state-changing
	// command. LockedUntil is RFC3339 and only populated on the
	// single response where a lockout was JUST set (handleAttack's
	// defeat branch) — any request made while already locked never
	// reaches buildStateResponse at all, since loadUserAndState and
	// CreateCharacter both reject it with 423 first.
	DeathMarks  int    `json:"death_marks"`
	LockedUntil string `json:"locked_until,omitempty"`

	// --- Tavern fields (see game/tavern.go) ---

	// Gold is always populated (0 for a character who's never earned
	// any) — every class can hold gold, unlike Mana/Spells above which
	// stay Mage-only.
	Gold int `json:"gold"`

	// AtTavern mirrors SaveState.AtTavern — the frontend uses this to
	// switch from dungeon commands (attack/cast/descend) to tavern
	// commands (see tavern.js), the same way it already branches on
	// RunComplete/InCombat/PendingAdvance today.
	AtTavern bool `json:"at_tavern"`

	// MonsterLore is only ever populated once the player has actually
	// paid to learn it (state.HasLearnedMonsterLore) — omitted
	// entirely otherwise, same "don't show dead/always-empty data"
	// convention Mana/Spells follow for non-Mage classes.
	MonsterLore []string `json:"monster_lore,omitempty"`

	// TavernSpells resolves state.CurrentTavernSpells (the two scroll
	// IDs currently on offer, out of game.ScrollSpells' 7) into full
	// display/inspect data plus price — only ever populated while
	// AtTavern is true, same "omit rather than show dead data"
	// convention MonsterLore follows. This replaces the frontend's old
	// hand-maintained static shop list (tavern.js's SHOP_ITEMS scroll
	// entries) now that which two spells are for sale changes every
	// visit instead of being fixed content.
	TavernSpells []tavernScrollResponse `json:"tavern_spells,omitempty"`

	// BlackjackRoundsPlayed/RouletteRoundsPlayed are always populated
	// (0 for a character who's never played either) — the frontend uses
	// these against game.BlackjackMaxRounds/game.RouletteMaxRounds to
	// show "rounds remaining" in the tavern menu, same idea as Gold
	// always being shown even at 0.
	BlackjackRoundsPlayed int `json:"blackjack_rounds_played"`
	RouletteRoundsPlayed  int `json:"roulette_rounds_played"`

	// BlackjackActive/BlackjackWager mirror the same-named SaveState
	// fields — only meaningful (and only populated) while a round is
	// actually in progress, same "omit rather than show dead data"
	// convention MonsterLore/Familiar follow. The frontend uses
	// BlackjackActive to know a bare "tavern blackjack" (no args)
	// should re-show the in-progress hand rather than explain how to
	// start a new one.
	BlackjackActive bool `json:"blackjack_active,omitempty"`
	BlackjackWager  int  `json:"blackjack_wager,omitempty"`

	// BlackjackPlayerCard/BlackjackDealerCards resolve
	// SaveState.BlackjackPlayerCards/BlackjackDealerCards for display.
	// The dealer's SECOND card is deliberately omitted while
	// BlackjackActive is true (only DealerCards[0], the "up card", is
	// sent) — the backend response is the only place this is enforced,
	// so there's no client-side trust to violate the way there would be
	// if the frontend had the hole card and just chose not to render
	// it. Once a round ends (BlackjackActive false but the round JUST
	// finished this response), the full dealer hand is exposed via the
	// narration `lines` instead, same as every other tavern outcome.
	BlackjackPlayerCards []string `json:"blackjack_player_cards,omitempty"`
	BlackjackDealerCards []string `json:"blackjack_dealer_cards,omitempty"`

	// Familiar is only populated while state.Familiar is non-"" — same
	// "omit rather than show dead data" convention as MonsterLore
	// above. Resolved into its display fields here (rather than
	// shipping the bare ID) so the frontend never needs its own copy
	// of familiars.go's Familiars table just to render a name/
	// description, matching how Spells is resolved server-side too.
	Familiar *familiarResponse `json:"familiar,omitempty"`

	// SecondFamiliar mirrors Familiar's resolved-display-fields
	// treatment above, for state.SecondFamiliar (the guaranteed
	// Ancient Woods companion — see familiars.go's
	// GrantSecondFamiliar). Only ever populated once earned, same
	// "omit rather than show dead data" convention as Familiar.
	SecondFamiliar *familiarResponse `json:"second_familiar,omitempty"`

	// DungeonComplete mirrors SaveState.DungeonComplete — the
	// frontend uses this (alongside AtTavern) to know when to offer
	// `tavern exit` instead of `tavern leave`, and RunComplete stays
	// false until the Journey (Stages 6-10) is ALSO finished — see
	// stages.go's doc comment on the Journey for the full boundary.
	DungeonComplete bool `json:"dungeon_complete"`

	// AtKingsChambers mirrors SaveState.AtKingsChambers — the
	// frontend uses this to know when to offer the Hall of Legacies /
	// path-choice flow instead of ordinary tavern commands, same role
	// AtTavern plays for tavern_* actions.
	AtKingsChambers bool `json:"at_kings_chambers"`

	// LegacyPath/LegacyPathName mirror SaveState.LegacyPath, resolved
	// to its display name server-side (see game.GetLegacyPath) — same
	// "server resolves display fields, client just renders them"
	// convention Familiar/SecondFamiliar already follow. Both stay
	// omitted until a path has actually been chosen.
	LegacyPath     string `json:"legacy_path,omitempty"`
	LegacyPathName string `json:"legacy_path_name,omitempty"`

	// Ability surfaces the character's class ability (Fighter's Second
	// Wind, Cleric's Mend, Ranger's Steady Aim, Rogue's Sneak Attack)
	// for display — always populated (every class has exactly one
	// ability, per classes.go), unlike Mana/Spells which stay omitted
	// for non-Mages. Usable is false for Mage (no "ability" action —
	// use "cast" instead) and for Rogue (Sneak Attack is passive, not
	// player-triggered) even though both still have a Name/Description
	// to show; for Fighter/Cleric/Ranger, Usable mirrors
	// !state.AbilityUsedThisStage.
	Ability *abilityResponse `json:"ability,omitempty"`
}

// abilityResponse is the public, display-only shape of a character's
// class ability — see gameStateResponse's Ability field doc comment
// above.
type abilityResponse struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Usable      bool   `json:"usable"`
}

// familiarResponse is the public, display-only shape of a
// game.Familiar — see gameStateResponse's Familiar field doc comment
// above. Kind is deliberately NOT exposed: it's an internal switch key
// for ResolveFamiliarAction, not something the frontend has any use
// for now that Name/Description already carry the flavor a player
// needs.
type familiarResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

// statsResponse mirrors game.Stats — a small, explicit response type
// rather than embedding game.Stats directly, matching the rest of
// this file's convention of never leaking an internal game.* type
// straight into the wire format (equippedResponse/inCombatResponse do
// the same for Equipped/InCombat).
type statsResponse struct {
	STR int `json:"str"`
	DEX int `json:"dex"`
	CON int `json:"con"`
}

type equippedResponse struct {
	WeaponID string `json:"weapon_id"`
	ArmorID  string `json:"armor_id"`
}

type inCombatResponse struct {
	EnemyID        string `json:"enemy_id,omitempty"`
	EnemyName      string `json:"enemy_name,omitempty"`
	EnemyCurrentHP int    `json:"enemy_current_hp,omitempty"`
	EnemyMaxHP     int    `json:"enemy_max_hp,omitempty"`

	// Enemies is non-empty ONLY for a simultaneous multi-enemy fight
	// (state.InCombat.Enemies, currently just the Stage 10/Part 3
	// ambush) — when it's set, EnemyID/EnemyName/EnemyCurrentHP/
	// EnemyMaxHP above are all omitted rather than populated with e.g.
	// the first enemy's data, so the frontend can't mistake a
	// three-enemy fight for an ordinary single-enemy one and render
	// just one HP bar.
	Enemies []ambushEnemyResponse `json:"enemies,omitempty"`
}

// ambushEnemyResponse is the public, per-enemy shape of one living
// combatant in a simultaneous multi-enemy fight — mirrors
// inCombatResponse's EnemyID/EnemyName/EnemyCurrentHP/EnemyMaxHP
// fields, just once per enemy instead of once per fight.
type ambushEnemyResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	CurrentHP int    `json:"current_hp"`
	MaxHP     int    `json:"max_hp"`
}

// spellResponse is the public, display-only shape of a game.Spell —
// see gameStateResponse's Spells field doc comment above. Beyond the
// original ID/Name/Description/ManaCost, it also carries the same
// mechanical detail game.Spell itself holds (Kind, AutoHit, damage
// dice, heal percent) so the frontend's "inspect" command can show a
// spell's real damage/effect numbers without needing its own copy of
// spells.go's data — same "server resolves, client just renders"
// convention Familiar/SecondFamiliar already follow elsewhere in this
// file. All of the Kind-specific fields are omitempty since only one
// branch (damage or heal) is ever populated for a given spell.
type spellResponse struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	ManaCost    int    `json:"mana_cost"`
	Kind        string `json:"kind,omitempty"`

	AutoHit        bool `json:"auto_hit,omitempty"`
	DamageDieSides int  `json:"damage_die_sides,omitempty"`
	DamageDieCount int  `json:"damage_die_count,omitempty"`

	HealPercentOfMaxHP int `json:"heal_percent_of_max_hp,omitempty"`
}

// newSpellResponse builds a spellResponse from a game.Spell, resolving
// every field this file's frontend might want to render or inspect —
// the one place that mapping happens, so Spells/TavernSpells below
// can't drift out of sync with each other.
func newSpellResponse(s game.Spell) spellResponse {
	return spellResponse{
		ID:                 s.ID,
		Name:               s.Name,
		Description:        s.Description,
		ManaCost:           s.ManaCost,
		Kind:               string(s.Kind),
		AutoHit:            s.AutoHit,
		DamageDieSides:     s.DamageDieSides,
		DamageDieCount:     s.DamageDieCount,
		HealPercentOfMaxHP: s.HealPercentOfMaxHP,
	}
}

// tavernScrollResponse is the public shape of one of the tavern's
// current scroll offerings — a spellResponse plus the gold price the
// frontend needs to render alongside it (see game.ScrollPrices).
type tavernScrollResponse struct {
	spellResponse
	Price int `json:"price"`
}

type encounterResponse struct {
	Stage         int    `json:"stage"`
	Part          int    `json:"part"`
	Description   string `json:"description"`
	IsStageFinale bool   `json:"is_stage_finale"`
}

// actionResultResponse wraps gameStateResponse with a Log of
// human-readable lines describing what just happened. These strings
// are rough placeholders — Day 10 (terminal output formatting) is
// where real presentation happens; this just guarantees the frontend
// always has *something* to print without re-deriving it from raw
// numbers.
type actionResultResponse struct {
	State gameStateResponse `json:"state"`
	Log   []string          `json:"log"`
}

// ---------------------------------------------------------------------
// Shared helpers
// ---------------------------------------------------------------------

// loadUserAndState is a shared helper for GET /api/game/state and
// POST /api/game/action — both need "authenticated user_id -> user
// row -> parsed SaveState, with hasCharacter true" before doing
// anything else. It writes the appropriate error response and returns
// ok=false on any failure, mirroring database/users.go's queryUser
// being a shared helper for two near-identical lookup methods.
//
// This is also where the death-cycle lockout (database.RecordDeath)
// is enforced for every game route: a locked account gets a 423
// before ANY action or state read proceeds, regardless of whether its
// JWT is still technically valid. CreateCharacter has its own
// identical check since it doesn't have a character yet to route
// through this helper.
func (h *GameHandler) loadUserAndState(w http.ResponseWriter, r *http.Request) (string, game.SaveState, int, bool) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		log.Printf("handlers: game: no user_id in context (route mounted outside auth middleware?)")
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return "", game.SaveState{}, 0, false
	}

	user, err := h.DB.GetUserByUserID(userID)
	if err != nil {
		if errors.Is(err, database.ErrUserNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return "", game.SaveState{}, 0, false
		}
		log.Printf("handlers: game: get user by user_id: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return "", game.SaveState{}, 0, false
	}

	if locked, msg := lockoutMessage(user.LockedUntil); locked {
		writeError(w, http.StatusLocked, msg)
		return "", game.SaveState{}, 0, false
	}

	state, hasCharacter, err := game.UnmarshalSaveState(user.SaveData)
	if err != nil {
		log.Printf("handlers: game: unmarshal save state for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return "", game.SaveState{}, 0, false
	}
	if !hasCharacter {
		writeError(w, http.StatusNotFound, "no character exists yet")
		return "", game.SaveState{}, 0, false
	}

	return userID, state, user.DeathMarks, true
}

// lockoutMessage reports whether lockedUntil (a unix timestamp, 0 if
// never locked) is still in the future, and if so, the message to
// show the player. Centralized here since loadUserAndState and
// CreateCharacter both need byte-for-byte the same check/wording.
func lockoutMessage(lockedUntil int64) (locked bool, message string) {
	if lockedUntil <= time.Now().Unix() {
		return false, ""
	}
	unlockAt := time.Unix(lockedUntil, 0).UTC()
	return true, fmt.Sprintf("account locked until %s after five deaths", unlockAt.Format(time.RFC3339))
}

// saveState marshals and persists state, logging (but not exposing)
// the underlying error. Both action branches and CreateCharacter fun
// nel through this one function so a marshal/DB failure is always
// handled the same way.
func (h *GameHandler) saveState(userID string, state *game.SaveState) error {
	saveData, err := game.MarshalSaveState(*state)
	if err != nil {
		log.Printf("handlers: game: marshal save state for %s: %v", userID, err)
		return err
	}
	if err := h.DB.UpdateGameState(userID, saveData, state.CurrentStage, state.IsRunComplete()); err != nil {
		log.Printf("handlers: game: update game state for %s: %v", userID, err)
		return err
	}
	return nil
}

// buildStateResponse converts a SaveState into its public response
// shape, resolving MaxHP, effective STR/DEX/CON, the in-progress enemy
// (if any), and the current encounter's flavor text (if the run isn't
// complete).
//
// deathMarks and lockedUntilUnix come from the caller rather than
// being looked up here, since every caller already has them on hand
// (loadUserAndState returns deathMarks; handleAttack's defeat branch
// has a just-updated pair straight from database.RecordDeath) — a
// second DB round-trip in here would risk showing stale marks in the
// one response that most needs to be fresh.
func (h *GameHandler) buildStateResponse(state game.SaveState, deathMarks int, lockedUntilUnix int64) (gameStateResponse, error) {
	maxHP, err := state.EffectiveMaxHP()
	if err != nil {
		return gameStateResponse{}, err
	}
	stats, err := state.EffectiveStats()
	if err != nil {
		return gameStateResponse{}, err
	}

	resp := gameStateResponse{
		CharacterName:  state.CharacterName,
		Class:          string(state.Class),
		Difficulty:     string(state.Difficulty),
		CurrentHP:      state.CurrentHP,
		MaxHP:          maxHP,
		Stats:          statsResponse{STR: stats.STR, DEX: stats.DEX, CON: stats.CON},
		CurrentStage:   state.CurrentStage,
		CurrentPart:    state.CurrentPart,
		Equipped:       equippedResponse{WeaponID: state.Equipped.WeaponID, ArmorID: state.Equipped.ArmorID},
		Inventory:      state.Inventory,
		PendingAdvance: state.PendingAdvance,
		RunComplete:    state.IsRunComplete(),
		DeathMarks:     deathMarks,
	}

	if lockedUntilUnix > time.Now().Unix() {
		resp.LockedUntil = time.Unix(lockedUntilUnix, 0).UTC().Format(time.RFC3339)
	}

	if state.InCombat != nil {
		if len(state.InCombat.Enemies) > 0 {
			// Simultaneous multi-enemy fight (the Stage 10/Part 3
			// ambush) — one entry per living-or-dead combatant, not
			// just the single enemy/enemy_name/current_hp/max_hp shape
			// below.
			var enemies []ambushEnemyResponse
			for _, ce := range state.InCombat.Enemies {
				if baseEnemy, ok := game.GetEnemy(ce.ID); ok {
					enemy := game.ApplyDifficulty(baseEnemy, state.Difficulty)
					enemies = append(enemies, ambushEnemyResponse{
						ID:        enemy.ID,
						Name:      enemy.Name,
						CurrentHP: ce.CurrentHP,
						MaxHP:     enemy.MaxHP,
					})
				}
			}
			resp.InCombat = &inCombatResponse{Enemies: enemies}
		} else if baseEnemy, ok := game.GetEnemy(state.InCombat.EnemyID); ok {
			enemy := game.ApplyDifficulty(baseEnemy, state.Difficulty)
			resp.InCombat = &inCombatResponse{
				EnemyID:        enemy.ID,
				EnemyName:      enemy.Name,
				EnemyCurrentHP: state.InCombat.EnemyCurrentHP,
				EnemyMaxHP:     enemy.MaxHP,
			}
		}
	}

	if !resp.RunComplete {
		if enc, ok := game.GetEncounter(state.CurrentStage, state.CurrentPart); ok {
			resp.Encounter = &encounterResponse{
				Stage:         enc.Stage,
				Part:          enc.Part,
				Description:   enc.Description,
				IsStageFinale: enc.IsStageFinale,
			}
		}
	}

	// Only a Mage has spells to cast (see handleCast) — surfacing
	// Mana/Spells for every other class would just be dead, always-0
	// data on the wire.
	if state.Class == game.ClassMage {
		resp.Mana = state.Mana
		for _, s := range game.MageSpells {
			resp.Spells = append(resp.Spells, newSpellResponse(s))
		}
		// Learned scroll spells (game/tavern.go's ScrollSpells) ride
		// along in the same list, appended after the starting kit —
		// the frontend's cast.js/inventory.js never need to know
		// "base kit" vs. "learned" is two different lists, they just
		// render whatever's in Spells, same as the server-decides/
		// client-renders convention everywhere else in this file.
		for _, learnedID := range state.LearnedSpells {
			if s, ok := game.GetScrollSpell(learnedID); ok {
				resp.Spells = append(resp.Spells, newSpellResponse(s))
			}
		}
	}

	resp.Gold = state.Gold
	resp.AtTavern = state.AtTavern
	resp.BlackjackRoundsPlayed = state.BlackjackRoundsPlayed
	resp.RouletteRoundsPlayed = state.RouletteRoundsPlayed
	resp.BlackjackActive = state.BlackjackActive
	if state.BlackjackActive {
		resp.BlackjackWager = state.BlackjackWager
		resp.BlackjackPlayerCards = state.BlackjackPlayerCards
		// Only the dealer's up card (index 0) goes out while the round
		// is still active — see BlackjackDealerCards' doc comment on
		// this being the enforcement point for keeping the hole card
		// hidden.
		if len(state.BlackjackDealerCards) > 0 {
			resp.BlackjackDealerCards = state.BlackjackDealerCards[:1]
		}
	}
	if state.HasLearnedMonsterLore {
		resp.MonsterLore = game.MonsterLore
	}
	// See TavernSpells' doc comment above: resolved fresh from
	// state.CurrentTavernSpells on every response rather than cached,
	// same "server is the source of truth, client just renders"
	// convention every other resolved field here follows.
	if state.AtTavern {
		for _, spellID := range state.CurrentTavernSpells {
			if s, ok := game.GetScrollSpell(spellID); ok {
				resp.TavernSpells = append(resp.TavernSpells, tavernScrollResponse{
					spellResponse: newSpellResponse(s),
					Price:         game.ScrollPrices[spellID],
				})
			}
		}
	}

	if state.Familiar != "" {
		if familiar, ok := game.GetFamiliar(state.Familiar); ok {
			resp.Familiar = &familiarResponse{
				ID:          familiar.ID,
				Name:        familiar.Name,
				Description: familiar.Description,
			}
		}
	}
	if state.SecondFamiliar != "" {
		if familiar, ok := game.GetFamiliar(state.SecondFamiliar); ok {
			resp.SecondFamiliar = &familiarResponse{
				ID:          familiar.ID,
				Name:        familiar.Name,
				Description: familiar.Description,
			}
		}
	}

	resp.DungeonComplete = state.DungeonComplete
	resp.AtKingsChambers = state.AtKingsChambers
	if state.LegacyPath != "" {
		resp.LegacyPath = state.LegacyPath
		if path, ok := game.GetLegacyPath(state.LegacyPath); ok {
			resp.LegacyPathName = path.Name
		}
	}

	// Ability is always populated once a class is known — every class
	// has exactly one (classes.go). Usable is false for Mage (spells
	// go through "cast", not "ability") and Rogue (Sneak Attack is
	// passive, no player-triggered action exists for it) regardless of
	// AbilityUsedThisStage, since neither of those two ever consumes
	// that flag in the first place.
	if class, ok := game.GetClass(state.Class); ok {
		usable := !state.AbilityUsedThisStage
		if class.ID == game.ClassMage || class.ID == game.ClassRogue {
			usable = false
		}
		resp.Ability = &abilityResponse{
			Name:        class.AbilityName,
			Description: class.AbilityDescription,
			Usable:      usable,
		}
	}

	return resp, nil
}

// respondWithState is the shared "save succeeded, tell the client
// what happened" tail for every action branch.
func (h *GameHandler) respondWithState(w http.ResponseWriter, state game.SaveState, deathMarks int, lockedUntilUnix int64, lines []string) {
	resp, err := h.buildStateResponse(state, deathMarks, lockedUntilUnix)
	if err != nil {
		log.Printf("handlers: game: build response: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	writeJSON(w, http.StatusOK, actionResultResponse{State: resp, Log: lines})
}

// describeAttack renders one AttackResult as a rough log line. Not
// meant to be the final player-facing copy — Day 10 owns real
// presentation. This just guarantees every action response carries
// *something* readable.
func describeAttack(attacker, defender string, result game.AttackResult) string {
	if !result.Hit {
		return fmt.Sprintf("%s rolls a %d and misses %s.", attacker, result.AttackRoll, defender)
	}
	if result.RolledOneHitKill {
		return fmt.Sprintf("%s finds a killing blow against %s!", attacker, defender)
	}
	if result.Crit {
		return fmt.Sprintf("%s rolls a %d — critical hit on %s for %d damage!", attacker, result.AttackRoll, defender, result.DamageDealt)
	}
	return fmt.Sprintf("%s rolls a %d and hits %s for %d damage.", attacker, result.AttackRoll, defender, result.DamageDealt)
}

// describeSpellAttack mirrors describeAttack for a non-AutoHit spell
// (i.e. one still resolved via game.ResolveAttack's normal d20-vs-AC
// roll) — same rough-placeholder role, just with the caster named as
// "You cast <spell>" instead of a weapon swing.
func describeSpellAttack(spellName, defender string, result game.AttackResult) string {
	if !result.Hit {
		return fmt.Sprintf("You cast %s and it fizzles against %s.", spellName, defender)
	}
	if result.Crit {
		return fmt.Sprintf("You cast %s — a devastating strike on %s for %d damage!", spellName, defender, result.DamageDealt)
	}
	return fmt.Sprintf("You cast %s and hit %s for %d damage.", spellName, defender, result.DamageDealt)
}

// statModifierFor mirrors game.combat.go's unexported
// resolveStatModifier — needed here too since handleCast has to
// derive a caster's damage stat modifier for a spell the same way
// BuildPlayerAttackProfile does for a weapon, but that helper isn't
// exported across the package boundary. Kept to the same closed
// str/dex set for the same reason: a typo'd stat name should fail
// loudly (falling through to 0) rather than silently miscalculating.
func statModifierFor(statName string, stats game.Stats) int {
	switch statName {
	case "str":
		return game.StatModifier(stats.STR)
	case "dex":
		return game.StatModifier(stats.DEX)
	default:
		return 0
	}
}

// ---------------------------------------------------------------------
// POST /api/game/create
// ---------------------------------------------------------------------

// CreateCharacter handles POST /api/game/create. Must be mounted
// behind auth.Middleware. Rejects the request outright if the account
// already has a character — there's no "overwrite my save" flow yet,
// and silently allowing one here would be an easy way to accidentally
// destroy progress.
func (h *GameHandler) CreateCharacter(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok {
		log.Printf("handlers: game: create: no user_id in context (route mounted outside auth middleware?)")
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return
	}

	user, err := h.DB.GetUserByUserID(userID)
	if err != nil {
		if errors.Is(err, database.ErrUserNotFound) {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		log.Printf("handlers: game: create: get user: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if locked, msg := lockoutMessage(user.LockedUntil); locked {
		writeError(w, http.StatusLocked, msg)
		return
	}

	_, hasCharacter, err := game.UnmarshalSaveState(user.SaveData)
	if err != nil {
		log.Printf("handlers: game: create: unmarshal existing save state for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if hasCharacter {
		writeError(w, http.StatusConflict, "a character already exists for this account")
		return
	}

	var req createCharacterRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	name := strings.TrimSpace(req.CharacterName)
	if name == "" {
		writeError(w, http.StatusBadRequest, "character_name is required")
		return
	}
	if len(name) > 32 {
		writeError(w, http.StatusBadRequest, "character_name must be 32 characters or fewer")
		return
	}

	state, err := game.NewSaveState(name, game.ClassID(req.Class), game.Difficulty(req.Difficulty))
	if err != nil {
		// NewSaveState's errors are always client-input problems
		// (unknown class/difficulty) — safe to surface directly since
		// they already name the bad field, unlike our generic
		// internal-error messages elsewhere.
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.respondWithState(w, state, user.DeathMarks, 0, []string{fmt.Sprintf("%s the %s descends into the dungeon.", state.CharacterName, state.Class)})
}

// ---------------------------------------------------------------------
// GET /api/game/state
// ---------------------------------------------------------------------

// GetState handles GET /api/game/state. Must be mounted behind
// auth.Middleware. Read-only — no state mutation, no DB write.
func (h *GameHandler) GetState(w http.ResponseWriter, r *http.Request) {
	_, state, deathMarks, ok := h.loadUserAndState(w, r)
	if !ok {
		return
	}

	resp, err := h.buildStateResponse(state, deathMarks, 0)
	if err != nil {
		log.Printf("handlers: game: get state: build response: %v", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

// ---------------------------------------------------------------------
// POST /api/game/action
// ---------------------------------------------------------------------

// Action handles POST /api/game/action. Must be mounted behind
// auth.Middleware. Dispatches to one of three private handlers based
// on req.Action — one endpoint, not three, since all three share
// identical load/save boilerplate and only differ in what they do to
// the loaded SaveState in between.
func (h *GameHandler) Action(w http.ResponseWriter, r *http.Request) {
	userID, state, deathMarks, ok := h.loadUserAndState(w, r)
	if !ok {
		return
	}

	var req actionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	switch req.Action {
	case "attack":
		h.handleAttack(w, userID, state, deathMarks, req.TargetID)
	case "ability":
		h.handleAbility(w, userID, state, deathMarks)
	case "equip":
		h.handleEquip(w, userID, state, deathMarks, req.ItemID)
	case "use":
		h.handleUse(w, userID, state, deathMarks, req.ItemID)
	case "cast":
		h.handleCast(w, userID, state, deathMarks, req.SpellID, req.TargetID)
	case "descend":
		h.handleDescend(w, userID, state, deathMarks)
	case "tavern_lore":
		h.handleTavernLore(w, userID, state, deathMarks)
	case "tavern_buy":
		h.handleTavernBuy(w, userID, state, deathMarks, req.ItemID)
	case "tavern_riddle":
		h.handleTavernRiddle(w, userID, state, deathMarks, req.Answer)
	case "tavern_blackjack":
		h.handleTavernBlackjack(w, userID, state, deathMarks, req.ItemID, req.Amount)
	case "tavern_roulette":
		h.handleTavernRoulette(w, userID, state, deathMarks, req.ItemID, req.Amount)
	case "tavern_leave":
		h.handleTavernLeave(w, userID, state, deathMarks)
	case "exit_dungeon":
		h.handleExitDungeon(w, userID, state, deathMarks)
	case "legacy_hall":
		h.handleLegacyHall(w, userID, state, deathMarks)
	case "choose_path":
		h.handleChoosePath(w, userID, state, deathMarks, req.ItemID)
	default:
		writeError(w, http.StatusBadRequest, "unknown action")
	}
}

// ensureInCombat spawns the current encounter's enemy into
// state.InCombat if the player isn't already mid-fight, leaving an
// existing fight untouched. Shared by handleAttack and handleCast —
// either a weapon swing or a spell cast can be the FIRST move of a
// fresh encounter, so both need identical spawn behavior rather than
// only "attack" being allowed to start a fight. Returns false (and
// logs) on a data-integrity failure the caller should turn into a 500.
func (h *GameHandler) ensureInCombat(userID string, state *game.SaveState) bool {
	if state.InCombat != nil {
		return true
	}

	encounter, ok := game.GetEncounter(state.CurrentStage, state.CurrentPart)
	if !ok {
		log.Printf("handlers: game: no encounter at stage %d part %d for %s", state.CurrentStage, state.CurrentPart, userID)
		return false
	}

	// Boss encounters (currently only Stage 5/Part 3) spawn the FIRST
	// boss phase rather than a normal Enemies-map lookup — see
	// boss.go's doc comment on why Stage 5/Part 3's EnemyID is the
	// BossEncounterID sentinel instead of a real enemy ID.
	if encounter.EnemyID == game.BossEncounterID {
		firstPhase := game.ApplyDifficulty(game.BossPhases[0], state.Difficulty)
		state.InCombat = &game.InCombat{EnemyID: firstPhase.ID, EnemyCurrentHP: firstPhase.MaxHP, BossPhase: 1}
		return true
	}

	// The Stage 10/Part 3 ambush (currently the only simultaneous
	// multi-enemy fight) spawns all three of game.AmbushEnemies at
	// once into InCombat.Enemies, rather than one at a time — see
	// state.go's InCombat.Enemies doc comment on why this is a
	// separate fight shape from every other encounter, boss included.
	if encounter.EnemyID == game.AmbushEncounterID {
		enemies := make([]game.CombatEnemy, 0, len(game.AmbushEnemies))
		for _, base := range game.AmbushEnemies {
			spawned := game.ApplyDifficulty(base, state.Difficulty)
			enemies = append(enemies, game.CombatEnemy{ID: spawned.ID, CurrentHP: spawned.MaxHP})
		}
		state.InCombat = &game.InCombat{Enemies: enemies}
		return true
	}

	baseEnemy, ok := game.GetEnemy(encounter.EnemyID)
	if !ok {
		log.Printf("handlers: game: unknown enemy %q in encounter for %s", encounter.EnemyID, userID)
		return false
	}
	// Spawn at the DIFFICULTY-SCALED max HP, not the base enemy's —
	// otherwise Hard mode would let the fight start already "won"
	// relative to the HP the client displays.
	spawned := game.ApplyDifficulty(baseEnemy, state.Difficulty)
	state.InCombat = &game.InCombat{EnemyID: spawned.ID, EnemyCurrentHP: spawned.MaxHP}
	return true
}

// manaPerWin returns how much Mana a single victory grants a Mage: 1
// before Stage 3, 2 from Stage 3 onward ("2 mana per battle won post
// tavern"). The tavern (game/tavern.go) sits exactly at the Stage 2 ->
// Stage 3 boundary and is the ONLY way to reach Stage 3 — AdvancePart
// (state.go) always bumps CurrentStage to 3 before handleDescend ever
// sets AtTavern true — so "CurrentStage >= 3" is a reliable, already-
// true-by-then proxy for "has passed the tavern." No fight can happen
// while AtTavern is true (handleAttack/handleCast both refuse), so by
// the time any victory is possible at Stage 3+, the tavern is
// necessarily behind the player, whether or not this exact visit's
// tavern_leave has been called — no separate persisted flag needed.
func manaPerWin(state game.SaveState) int {
	if state.CurrentStage >= 3 {
		return 2
	}
	return 1
}

// grantVictoryRewards appends the "enemy defeated" bookkeeping shared
// by a killing weapon attack and a killing spell cast: clearing
// combat, flagging PendingAdvance, and — Mage only — the Mana earned
// per won fight (see manaPerWin above for the post-tavern rate bump).
// Only called for a FINAL kill — i.e. either an ordinary enemy, or the
// last phase of the boss fight; a non-final boss-phase kill goes
// through advanceBossPhase below instead, which does NOT clear combat.
func grantVictoryRewards(rng *rand.Rand, class game.Class, state *game.SaveState, enemy game.Enemy, lines []string) []string {
	lines = append(lines, fmt.Sprintf("%s is defeated!", enemy.Name))
	state.InCombat = nil
	state.PendingAdvance = true

	if class.ID == game.ClassMage {
		state.Mana += manaPerWin(*state)
		lines = append(lines, "You feel your mana grow.")
	}

	// Gold drop: flat chance, every class, every kill — see
	// game.RollGoldDrop's doc comment on why this isn't gated the way
	// Mana is above.
	if goldGained := game.RollGoldDrop(rng); goldGained > 0 {
		state.Gold += goldGained
		lines = append(lines, fmt.Sprintf("You find %d gold on the body.", goldGained))
	}

	lines = rollAndGrantFamiliar(rng, state, lines)

	return lines
}

// rollAndGrantFamiliar checks for a familiar drop on a just-defeated
// enemy and, if one lands, bonds it to state and appends its
// DisplayFlavor line. Shared by grantVictoryRewards and
// advanceBossPhase so a boss-phase kill is exactly as eligible a drop
// source as any ordinary kill — see RollFamiliarDrop's doc comment for
// the stage gate and the "already have one" suppression, both
// enforced there rather than here.
func rollAndGrantFamiliar(rng *rand.Rand, state *game.SaveState, lines []string) []string {
	famID, dropped := game.RollFamiliarDrop(rng, state.CurrentStage, state.Familiar != "")
	if !dropped {
		return lines
	}
	state.Familiar = famID
	if familiar, ok := game.GetFamiliar(famID); ok {
		lines = append(lines, familiar.DisplayFlavor)
	}
	return lines
}

// advanceBossPhase handles defeating a NON-FINAL boss phase (BossPhase
// 1 or 2 out of len(game.BossPhases)==3). Unlike grantVictoryRewards,
// it deliberately does NOT clear state.InCombat or set PendingAdvance
// — the fight continues immediately into the next phase, which spawns
// at its own full HP the same way ensureInCombat spawns phase 1 (see
// boss.go's doc comment on each phase being its own Enemy entry, and
// its NOTE that this transition logic was the missing piece keeping
// phases 2/3 unreachable). Mana/gold are still granted per phase
// defeated, same as any other kill — a phase clear is a real victory
// in its own right, not a formality on the way to one.
func advanceBossPhase(rng *rand.Rand, class game.Class, state *game.SaveState, defeatedPhase game.Enemy, lines []string) []string {
	lines = append(lines, fmt.Sprintf("%s falls, but the fight isn't over.", defeatedPhase.Name))

	if class.ID == game.ClassMage {
		state.Mana += manaPerWin(*state)
		lines = append(lines, "You feel your mana grow.")
	}
	if goldGained := game.RollGoldDrop(rng); goldGained > 0 {
		state.Gold += goldGained
		lines = append(lines, fmt.Sprintf("You find %d gold on the body.", goldGained))
	}

	lines = rollAndGrantFamiliar(rng, state, lines)

	// state.InCombat.BossPhase is 1-based (1/2/3), so it's also exactly
	// the zero-based index of the NEXT phase in BossPhases — e.g.
	// defeating phase 1 (BossPhase==1) spawns BossPhases[1], phase 2.
	nextIndex := state.InCombat.BossPhase
	nextPhase := game.ApplyDifficulty(game.BossPhases[nextIndex], state.Difficulty)
	state.InCombat = &game.InCombat{
		EnemyID:        nextPhase.ID,
		EnemyCurrentHP: nextPhase.MaxHP,
		BossPhase:      nextIndex + 1,
	}
	lines = append(lines, fmt.Sprintf("%s rises to take its place.", nextPhase.Name))

	return lines
}

// resolveFamiliarMiniTurn runs every bonded familiar's mini-turn for
// one combat round — see familiars.go's package doc comment on this
// happening alongside, not instead of, the player's own action.
// Called once per round from handleAttack/handleCast, but ONLY when
// the enemy survived the player's own action (a dead enemy has
// nothing left for a familiar to act against); the player's Killed
// branches in both handlers never reach this.
//
// A character can hold up to two familiars at once (state.Familiar,
// the ordinary random-drop primary, and state.SecondFamiliar, the
// guaranteed Ancient Woods companion — see familiars.go's
// GrantSecondFamiliar) — this loops over whichever slots are
// non-empty, in primary-then-secondary order, stopping early the
// moment the enemy is defeated so a second familiar never gets a
// mini-turn against an already-dead target.
//
// blockNextHit is true if EITHER familiar's mini-turn produced one —
// two separate small chances to block the counterattack, not one
// merged higher chance, since each is exactly the per-familiar roll
// ResolveFamiliarAction already returns.
//
// A familiar mini-turn can itself finish the enemy off (Bloodleech,
// Hexmoth, Little Reaper, Stormtail all deal damage) — enemyDefeated
// reports that so the caller skips resolveEnemyCounterattack entirely
// rather than letting an already-dead enemy take one last swing.
func resolveFamiliarMiniTurn(rng *rand.Rand, class game.Class, state *game.SaveState, enemy game.Enemy, maxHP int, lines []string) (updatedLines []string, blockNextHit bool, enemyDefeated bool) {
	for _, familiarID := range activeFamiliarIDs(state) {
		result := game.ResolveFamiliarAction(rng, familiarID, enemy.Name, state.InCombat.EnemyCurrentHP, enemy.MaxHP)
		lines = append(lines, result.Lines...)

		if result.DamageToEnemy > 0 {
			state.InCombat.EnemyCurrentHP -= result.DamageToEnemy
			if state.InCombat.EnemyCurrentHP < 0 {
				state.InCombat.EnemyCurrentHP = 0
			}
		}
		if result.HealToPlayer > 0 {
			state.CurrentHP += result.HealToPlayer
			if state.CurrentHP > maxHP {
				state.CurrentHP = maxHP
			}
		}
		// ApplyPoison/Burn/Stun reuse the exact same InCombat counters
		// a weapon condition would set, including the same duration
		// (see items.go's StatusEffectDuration doc comment) — a
		// familiar landing an ailment is indistinguishable from a
		// weapon landing one from this point on, ticked down the same
		// way in resolveEnemyCounterattack below.
		if result.ApplyPoison {
			state.InCombat.PoisonTurns = game.StatusEffectDuration
		}
		if result.ApplyBurn {
			state.InCombat.BurnTurns = game.StatusEffectDuration
		}
		if result.ApplyStun {
			state.InCombat.StunTurns = game.StatusEffectDuration
		}
		if result.BlockNextHit {
			blockNextHit = true
		}

		if state.InCombat.EnemyCurrentHP <= 0 {
			if state.InCombat.BossPhase > 0 && state.InCombat.BossPhase < len(game.BossPhases) {
				lines = advanceBossPhase(rng, class, state, enemy, lines)
			} else {
				lines = grantVictoryRewards(rng, class, state, enemy, lines)
			}
			return lines, false, true
		}
	}

	return lines, blockNextHit, false
}

// activeFamiliarIDs returns state's bonded familiar IDs, primary
// first (state.Familiar), then secondary (state.SecondFamiliar) if
// present — skipping either slot that's empty. Centralized here so
// resolveFamiliarMiniTurn's loop and any future per-familiar logic
// don't each need their own "collect the non-empty slots" boilerplate.
func activeFamiliarIDs(state *game.SaveState) []string {
	var ids []string
	if state.Familiar != "" {
		ids = append(ids, state.Familiar)
	}
	if state.SecondFamiliar != "" {
		ids = append(ids, state.SecondFamiliar)
	}
	return ids
}

// applyWeaponAilments reads AppliedPoison/AppliedBurn/AppliedStun off
// an already-resolved AttackResult and, for whichever fired, sets the
// matching InCombat counter to game.StatusEffectDuration — the exact
// same counters a familiar's ApplyPoison/Burn/Stun sets in
// resolveFamiliarMiniTurn above, so a weapon condition and a familiar
// ailment are indistinguishable once landed, both read back the same
// way by resolveEnemyCounterattack's ticking below. Shared by
// handleAttack's weapon swing and handleCast's SpellKindDamage
// branch, since both produce a game.AttackResult the same way.
func applyWeaponAilments(state *game.SaveState, result game.AttackResult, enemyName string, lines []string) []string {
	if result.AppliedPoison {
		state.InCombat.PoisonTurns = game.StatusEffectDuration
		lines = append(lines, fmt.Sprintf("%s is poisoned!", enemyName))
	}
	if result.AppliedBurn {
		state.InCombat.BurnTurns = game.StatusEffectDuration
		lines = append(lines, fmt.Sprintf("%s is set ablaze!", enemyName))
	}
	if result.AppliedStun {
		state.InCombat.StunTurns = game.StatusEffectDuration
		lines = append(lines, fmt.Sprintf("%s is stunned!", enemyName))
	}
	return lines
}

// decrementAilment steps one of InCombat's PoisonTurns/BurnTurns/
// StunTurns counters down by one round, floored at 0 — the one place
// that countdown arithmetic happens, so resolveEnemyCounterattack's
// three call sites can't drift out of sync with each other over an
// off-by-one.
func decrementAilment(turns int) int {
	if turns <= 0 {
		return 0
	}
	return turns - 1
}

// ---------------------------------------------------------------------
// Stage 10/Part 3 ambush (game.AmbushEncounterID) — simultaneous
// 3-enemy fight helpers.
//
// Every function below operates on state.InCombat.Enemies
// (game.CombatEnemy, state.go) instead of InCombat's flat EnemyID/
// EnemyCurrentHP/ailment fields. This is a deliberately separate code
// path from the rest of this file, not a generalization of it — every
// other encounter (all 17 ordinary fights plus the 3-phase final
// boss) never touches any function in this section, and these
// functions never touch InCombat.EnemyID/EnemyCurrentHP/BossPhase.
// See state.go's InCombat.Enemies doc comment for the full reasoning.
// ---------------------------------------------------------------------

// allAmbushEnemiesDead reports whether every enemy in an ambush fight
// has been reduced to 0 HP — the ambush's sole victory condition,
// checked after every action (the player's own turn, the familiar's
// mini-turn, and each enemy counterattack) that could have just
// finished the last one off.
func allAmbushEnemiesDead(state *game.SaveState) bool {
	if state.InCombat == nil {
		return true
	}
	for _, ce := range state.InCombat.Enemies {
		if ce.CurrentHP > 0 {
			return false
		}
	}
	return true
}

// lowestHPLivingAmbushEnemy returns the index of the living ambush
// enemy with the least current HP, or (0, false) if none are left
// alive. Used both as resolveAmbushTarget's no-target-named default
// (an old "just type attack" habit still resolves sensibly) and as
// the familiar mini-turn's fallback target when the player's own
// target already died this round.
func lowestHPLivingAmbushEnemy(state *game.SaveState) (int, bool) {
	idx := -1
	for i, ce := range state.InCombat.Enemies {
		if ce.CurrentHP <= 0 {
			continue
		}
		if idx == -1 || ce.CurrentHP < state.InCombat.Enemies[idx].CurrentHP {
			idx = i
		}
	}
	return idx, idx != -1
}

// ambushEnemyDisplay resolves one InCombat.Enemies index into its
// full, difficulty-scaled game.Enemy (name, stats, max HP, etc.) —
// CombatEnemy itself only stores the bare ID + current HP, same
// "re-derive from the base definition every read" convention
// game.ApplyDifficulty's doc comment describes for the single-enemy
// path.
func ambushEnemyDisplay(state *game.SaveState, idx int) (game.Enemy, bool) {
	if idx < 0 || idx >= len(state.InCombat.Enemies) {
		return game.Enemy{}, false
	}
	baseEnemy, ok := game.GetEnemy(state.InCombat.Enemies[idx].ID)
	if !ok {
		return game.Enemy{}, false
	}
	return game.ApplyDifficulty(baseEnemy, state.Difficulty), true
}

// resolveAmbushTarget picks which living ambush enemy this round's
// player action should hit. An empty targetID (an old "just type
// attack" habit, or a heal spell that has no target of its own)
// defaults to the lowest-HP living enemy. A non-empty targetID must
// name a KNOWN ambush enemy that is NOT already defeated — either
// failure returns a specific, player-facing errMsg rather than
// silently falling back to some other target, since guessing what the
// player meant to attack is worse than telling them clearly why their
// named target didn't resolve.
func resolveAmbushTarget(state *game.SaveState, targetID string) (idx int, errMsg string) {
	if targetID == "" {
		lowest, ok := lowestHPLivingAmbushEnemy(state)
		if !ok {
			return 0, "no living enemies remain"
		}
		return lowest, ""
	}

	found := -1
	for i, ce := range state.InCombat.Enemies {
		if ce.ID == targetID {
			found = i
			break
		}
	}
	if found == -1 {
		return 0, fmt.Sprintf("unknown target %q — try thaddeus, alfonse, or aragorn", targetID)
	}
	if state.InCombat.Enemies[found].CurrentHP <= 0 {
		name := targetID
		if baseEnemy, ok := game.GetEnemy(targetID); ok {
			name = baseEnemy.Name
		}
		return 0, fmt.Sprintf("%s is already defeated", name)
	}
	return found, ""
}

// applyAmbushWeaponAilments mirrors applyWeaponAilments exactly, but
// writes into one CombatEnemy's own ailment counters instead of
// InCombat's flat fields — see CombatEnemy's doc comment (state.go)
// on why ailments are tracked per-enemy once more than one enemy can
// be independently poisoned/burned/stunned at once.
func applyAmbushWeaponAilments(ce *game.CombatEnemy, result game.AttackResult, enemyName string, lines []string) []string {
	if result.AppliedPoison {
		ce.PoisonTurns = game.StatusEffectDuration
		lines = append(lines, fmt.Sprintf("%s is poisoned!", enemyName))
	}
	if result.AppliedBurn {
		ce.BurnTurns = game.StatusEffectDuration
		lines = append(lines, fmt.Sprintf("%s is set ablaze!", enemyName))
	}
	if result.AppliedStun {
		ce.StunTurns = game.StatusEffectDuration
		lines = append(lines, fmt.Sprintf("%s is stunned!", enemyName))
	}
	return lines
}

// resolveAmbushFamiliarMiniTurn runs every bonded familiar's mini-turn
// against ONE specific ambush enemy for this round — the one the
// player just targeted, or (see resolveAmbushRound) whichever living
// enemy has the least HP if that one already died to the player's own
// action. With three simultaneous targets available, "whichever one
// the player's own turn just involved" is the only non-arbitrary
// choice; a familiar picking its own independent target every round
// would be no more meaningful than picking at random.
//
// Mirrors resolveFamiliarMiniTurn's mechanics exactly (same
// ResolveFamiliarAction call, same ailment counters, same
// BlockNextHit) but writes into the target CombatEnemy's own fields
// and never itself grants victory rewards — the caller
// (resolveAmbushRound) checks allAmbushEnemiesDead once, after this
// mini-turn returns, rather than this function re-deriving "is the
// WHOLE fight over" on every familiar in the loop.
func resolveAmbushFamiliarMiniTurn(rng *rand.Rand, state *game.SaveState, targetIdx int, targetEnemy game.Enemy, maxHP int, lines []string) (updatedLines []string, blockNextHit bool, targetDefeated bool) {
	ce := &state.InCombat.Enemies[targetIdx]
	for _, familiarID := range activeFamiliarIDs(state) {
		if ce.CurrentHP <= 0 {
			break
		}
		result := game.ResolveFamiliarAction(rng, familiarID, targetEnemy.Name, ce.CurrentHP, targetEnemy.MaxHP)
		lines = append(lines, result.Lines...)

		if result.DamageToEnemy > 0 {
			ce.CurrentHP -= result.DamageToEnemy
			if ce.CurrentHP < 0 {
				ce.CurrentHP = 0
			}
		}
		if result.HealToPlayer > 0 {
			state.CurrentHP += result.HealToPlayer
			if state.CurrentHP > maxHP {
				state.CurrentHP = maxHP
			}
		}
		if result.ApplyPoison {
			ce.PoisonTurns = game.StatusEffectDuration
		}
		if result.ApplyBurn {
			ce.BurnTurns = game.StatusEffectDuration
		}
		if result.ApplyStun {
			ce.StunTurns = game.StatusEffectDuration
		}
		if result.BlockNextHit {
			blockNextHit = true
		}
	}

	if ce.CurrentHP <= 0 {
		lines = append(lines, fmt.Sprintf("%s is defeated!", targetEnemy.Name))
		return lines, blockNextHit, true
	}
	return lines, blockNextHit, false
}

// grantAmbushVictoryRewards is grantVictoryRewards' ambush counterpart
// — same Mana/Gold/familiar-drop bookkeeping, same InCombat-clearing
// and PendingAdvance-setting, just without a single defeated Enemy to
// name (each of the three already got its own "%s is defeated!" line
// as it died, from whichever call site — player attack, familiar
// mini-turn, burn tick, or counterattack-round Mirror Ward reflect —
// actually finished it off).
func grantAmbushVictoryRewards(rng *rand.Rand, class game.Class, state *game.SaveState, lines []string) []string {
	lines = append(lines, "The ambush falls silent. Whatever Thaddeus meant to say, he never gets the chance.")
	state.InCombat = nil
	state.PendingAdvance = true

	if class.ID == game.ClassMage {
		state.Mana += manaPerWin(*state)
		lines = append(lines, "You feel your mana grow.")
	}
	if goldGained := game.RollGoldDrop(rng); goldGained > 0 {
		state.Gold += goldGained
		lines = append(lines, fmt.Sprintf("You find %d gold among the bodies.", goldGained))
	}

	lines = rollAndGrantFamiliar(rng, state, lines)

	return lines
}

// resolveAmbushRound resolves everything that happens AFTER the
// player's own action in an ambush round: the familiar mini-turn,
// then every still-living enemy's counterattack (all three, if all
// three are still up — a chaotic pile-on, not a one-at-a-time
// rotation), then a final victory check. Shared by handleAmbushAttack
// and handleAmbushCast, mirroring resolveEnemyCounterattack's shared
// role for the single-enemy path — the caller has already applied the
// player's weapon swing or spell to state.InCombat.Enemies by the time
// this runs.
//
// targetIdx is the enemy the player's own action just targeted (or
// would have targeted, for a non-damage spell) — used to pick the
// familiar mini-turn's target and to know which enemy blockNextHit (if
// any) should negate.
func (h *GameHandler) resolveAmbushRound(
	userID string,
	class game.Class,
	state *game.SaveState,
	deathMarks int,
	rng *rand.Rand,
	playerAC int,
	maxHP int,
	targetIdx int,
	lines []string,
) (updatedDeathMarks int, lockedUntilUnix int64, updatedLines []string, fatal bool) {
	if allAmbushEnemiesDead(state) {
		lines = grantAmbushVictoryRewards(rng, class, state, lines)
		return deathMarks, 0, lines, false
	}

	// Familiar mini-turn: same target the player just acted against,
	// unless that one's already dead, in which case it redirects to
	// whichever living enemy has the least HP (see
	// resolveAmbushFamiliarMiniTurn's doc comment).
	familiarTargetIdx := targetIdx
	if familiarTargetIdx < 0 || familiarTargetIdx >= len(state.InCombat.Enemies) || state.InCombat.Enemies[familiarTargetIdx].CurrentHP <= 0 {
		idx, ok := lowestHPLivingAmbushEnemy(state)
		if !ok {
			lines = grantAmbushVictoryRewards(rng, class, state, lines)
			return deathMarks, 0, lines, false
		}
		familiarTargetIdx = idx
	}

	familiarTargetEnemy, ok := ambushEnemyDisplay(state, familiarTargetIdx)
	if !ok {
		log.Printf("handlers: game: ambush round: unknown ambush enemy id for %s", userID)
		return deathMarks, 0, lines, true
	}

	var blockNextHit bool
	lines, blockNextHit, _ = resolveAmbushFamiliarMiniTurn(rng, state, familiarTargetIdx, familiarTargetEnemy, maxHP, lines)

	if allAmbushEnemiesDead(state) {
		lines = grantAmbushVictoryRewards(rng, class, state, lines)
		return deathMarks, 0, lines, false
	}

	// Every still-living enemy strikes back this round. blockNextHit
	// (an Aegis Whelp/Wandering Wisp mini-turn result) negates only
	// the one enemy the familiar just acted against — the other
	// living enemies still swing, same as a shield turning aside one
	// specific attacker's blow wouldn't stop the other two from
	// hitting at all in a real 3-on-1.
	for i := range state.InCombat.Enemies {
		ce := &state.InCombat.Enemies[i]
		if ce.CurrentHP <= 0 {
			continue
		}

		baseEnemy, ok := game.GetEnemy(ce.ID)
		if !ok {
			log.Printf("handlers: game: ambush round: unknown ambush enemy %q for %s", ce.ID, userID)
			return deathMarks, 0, lines, true
		}
		enemy := game.ApplyDifficulty(baseEnemy, state.Difficulty)

		// Burn ticks first, independent of hit/miss — same ordering as
		// resolveEnemyCounterattack's single-enemy version.
		if ce.BurnTurns > 0 {
			ce.CurrentHP -= game.BurnDamagePerTurn
			if ce.CurrentHP < 0 {
				ce.CurrentHP = 0
			}
			lines = append(lines, fmt.Sprintf("The burn sears %s for %d damage.", enemy.Name, game.BurnDamagePerTurn))
			if ce.CurrentHP <= 0 {
				lines = append(lines, fmt.Sprintf("%s is defeated!", enemy.Name))
				continue
			}
		}

		poisonedThisRound := ce.PoisonTurns > 0
		stunnedThisRound := ce.StunTurns > 0
		ce.PoisonTurns = decrementAilment(ce.PoisonTurns)
		ce.BurnTurns = decrementAilment(ce.BurnTurns)
		ce.StunTurns = decrementAilment(ce.StunTurns)

		if stunnedThisRound {
			lines = append(lines, fmt.Sprintf("%s is still reeling and can't strike back.", enemy.Name))
			continue
		}

		if blockNextHit && i == familiarTargetIdx {
			lines = append(lines, fmt.Sprintf("The blow never reaches you — %s's strike is turned aside completely.", enemy.Name))
			continue
		}

		enemyProfile, err := game.BuildEnemyAttackProfile(enemy)
		if err != nil {
			log.Printf("handlers: game: ambush round: build enemy profile for %s: %v", userID, err)
			return deathMarks, 0, lines, true
		}
		if poisonedThisRound {
			enemyProfile.AttackStatModifier -= game.PoisonAttackPenalty
		}

		enemyResult := game.ResolveAttack(rng, enemyProfile, playerAC, state.CurrentHP)
		lines = append(lines, describeAttack(enemy.Name, "you", enemyResult))

		if enemyResult.Hit {
			state.CurrentHP -= enemyResult.DamageDealt
			if state.CurrentHP < 0 {
				state.CurrentHP = 0
			}
			if enemyResult.LifestealHealAmount > 0 {
				ce.CurrentHP += enemyResult.LifestealHealAmount
				if ce.CurrentHP > enemy.MaxHP {
					ce.CurrentHP = enemy.MaxHP
				}
			}

			// Mirror Ward reflects against WHICHEVER enemy just landed
			// a hit — checked fresh for each attacker in this loop, not
			// once for the round, since all three enemies are eligible
			// to trigger it independently this round.
			hasMirrorWard := false
			for _, familiarID := range activeFamiliarIDs(state) {
				if familiar, ok := game.GetFamiliar(familiarID); ok && familiar.Kind == game.FamiliarKindMirror {
					hasMirrorWard = true
					break
				}
			}
			if hasMirrorWard {
				reflect := enemyResult.DamageDealt / 2
				if reflect < 1 {
					reflect = 1
				}
				ce.CurrentHP -= reflect
				if ce.CurrentHP < 0 {
					ce.CurrentHP = 0
				}
				lines = append(lines, fmt.Sprintf("Your Mirror Ward flares and hurls %d damage back at %s!", reflect, enemy.Name))
				if ce.CurrentHP <= 0 {
					lines = append(lines, fmt.Sprintf("%s is defeated!", enemy.Name))
				}
			}
		}

		if enemyResult.Killed {
			newMarks, newLockedUntil, derr := h.DB.RecordDeath(userID)
			if derr != nil {
				log.Printf("handlers: game: record death for %s: %v", userID, derr)
				return deathMarks, 0, lines, true
			}
			deathMarks = newMarks
			lockedUntilUnix = newLockedUntil

			state.InCombat = nil
			state.CurrentHP = maxHP

			if newLockedUntil > 0 {
				unlockAt := time.Unix(newLockedUntil, 0).UTC()
				lines = append(lines,
					"Death marks you for the fifth time.",
					fmt.Sprintf("You are cast out of the dungeon, locked out until %s.", unlockAt.Format(time.RFC3339)),
				)
			} else {
				lines = append(lines, fmt.Sprintf("You are defeated and stagger back to recover... (death mark %d/5)", newMarks))
			}

			return deathMarks, lockedUntilUnix, lines, false
		}
	}

	if allAmbushEnemiesDead(state) {
		lines = grantAmbushVictoryRewards(rng, class, state, lines)
	}

	return deathMarks, lockedUntilUnix, lines, false
}

// resolveEnemyCounterattack runs the enemy's strike-back after the
// player's turn — whether that turn was a weapon attack or a spell
// cast — resolves without killing it. Shared by handleAttack and
// handleCast so the death-mark/lockout bookkeeping (database.RecordDeath,
// the 24h lockout) can't drift out of sync between the two ways a
// player's turn can end. fatal reports an internal error the caller
// should turn into a 500 (500-and-return, not surfaced as a normal
// game-log line).
//
// This is also where the enemy's active status ailments (items.go's
// ConditionPoison/Burn/Stun, whether landed by a weapon via
// applyWeaponAilments or by a familiar via resolveFamiliarMiniTurn)
// actually take effect and tick down: burn deals its fixed damage at
// the top of the round, poison weakens this round's attack roll, and
// stun skips the swing outright — see each one's inline comment below
// for exactly where.
//
// blockNextHit, if true (Aegis Whelp / Wandering Wisp — see
// resolveFamiliarMiniTurn), fully negates this counterattack: the
// enemy's swing is never even rolled, matching FamiliarActionResult's
// doc comment that BlockNextHit is read and consumed here, not
// persisted between rounds.
func (h *GameHandler) resolveEnemyCounterattack(
	userID string,
	class game.Class,
	state *game.SaveState,
	deathMarks int,
	rng *rand.Rand,
	enemy game.Enemy,
	enemyProfile game.AttackProfile,
	playerAC int,
	maxHP int,
	blockNextHit bool,
	lines []string,
) (updatedDeathMarks int, lockedUntilUnix int64, updatedLines []string, fatal bool) {
	// Burn ticks at the start of the round, independent of any hit/miss
	// roll (see items.go's ConditionBurn doc comment) — checked before
	// anything else this round since a burn tick can end the fight on
	// its own, before the enemy ever gets a chance to swing back.
	if state.InCombat.BurnTurns > 0 {
		state.InCombat.EnemyCurrentHP -= game.BurnDamagePerTurn
		if state.InCombat.EnemyCurrentHP < 0 {
			state.InCombat.EnemyCurrentHP = 0
		}
		lines = append(lines, fmt.Sprintf("The burn sears %s for %d damage.", enemy.Name, game.BurnDamagePerTurn))
		if state.InCombat.EnemyCurrentHP <= 0 {
			if state.InCombat.BossPhase > 0 && state.InCombat.BossPhase < len(game.BossPhases) {
				lines = advanceBossPhase(rng, class, state, enemy, lines)
			} else {
				lines = grantVictoryRewards(rng, class, state, enemy, lines)
			}
			return deathMarks, 0, lines, false
		}
	}

	// All three ailments tick down exactly once per round from here —
	// captured (poisonedThisRound/stunnedThisRound) BEFORE decrementing
	// so a counter that's about to expire (1 -> 0) still applies its
	// effect for this, its final, round.
	poisonedThisRound := state.InCombat.PoisonTurns > 0
	stunnedThisRound := state.InCombat.StunTurns > 0
	state.InCombat.PoisonTurns = decrementAilment(state.InCombat.PoisonTurns)
	state.InCombat.BurnTurns = decrementAilment(state.InCombat.BurnTurns)
	state.InCombat.StunTurns = decrementAilment(state.InCombat.StunTurns)

	if blockNextHit {
		lines = append(lines, fmt.Sprintf("The blow never reaches you — %s's strike is turned aside completely.", enemy.Name))
		return deathMarks, 0, lines, false
	}

	if stunnedThisRound {
		lines = append(lines, fmt.Sprintf("%s is still reeling and can't strike back.", enemy.Name))
		return deathMarks, 0, lines, false
	}

	// Poison weakens the enemy's own attack roll while active — a flat
	// penalty on AttackStatModifier, not baked into the enemy's stats
	// (see items.go's ConditionPoison/PoisonAttackPenalty doc
	// comments). enemyProfile is a value, not a pointer, so this
	// mutation is local to this call and never leaks back into the
	// enemy's base definition.
	if poisonedThisRound {
		enemyProfile.AttackStatModifier -= game.PoisonAttackPenalty
	}

	enemyResult := game.ResolveAttack(rng, enemyProfile, playerAC, state.CurrentHP)
	lines = append(lines, describeAttack(enemy.Name, "you", enemyResult))

	if enemyResult.Hit {
		state.CurrentHP -= enemyResult.DamageDealt
		if state.CurrentHP < 0 {
			state.CurrentHP = 0
		}
		if enemyResult.LifestealHealAmount > 0 {
			state.InCombat.EnemyCurrentHP += enemyResult.LifestealHealAmount
			if state.InCombat.EnemyCurrentHP > enemy.MaxHP {
				state.InCombat.EnemyCurrentHP = enemy.MaxHP
			}
		}

		// Mirror Ward is passive on its own mini-turn (see
		// ResolveFamiliarAction's FamiliarKindMirror case) — its
		// entire effect lives here instead, reflecting half of
		// whatever damage the enemy just landed on the player back
		// onto the enemy. Checked after LifestealHealAmount above so
		// a lifesteal enemy's heal and the reflect answering it both
		// land against the enemy's CURRENT (post-lifesteal) HP.
		//
		// Checks BOTH familiar slots (activeFamiliarIDs), not just
		// state.Familiar — Mirror Ward can just as easily be the
		// SECOND familiar (granted at the Journey's Ancient Woods
		// finale, see familiars.go's GrantSecondFamiliar) as the
		// primary one, and its passive reflect has to fire either way.
		hasMirrorWard := false
		for _, familiarID := range activeFamiliarIDs(state) {
			if familiar, ok := game.GetFamiliar(familiarID); ok && familiar.Kind == game.FamiliarKindMirror {
				hasMirrorWard = true
				break
			}
		}
		if hasMirrorWard {
			reflect := enemyResult.DamageDealt / 2
			if reflect < 1 {
				reflect = 1
			}
			state.InCombat.EnemyCurrentHP -= reflect
			if state.InCombat.EnemyCurrentHP < 0 {
				state.InCombat.EnemyCurrentHP = 0
			}
			lines = append(lines, fmt.Sprintf("Your Mirror Ward flares and hurls %d damage back at %s!", reflect, enemy.Name))
		}
	}

	if enemyResult.Killed {
		// Defeat handling: every death adds one mark
		// (database.RecordDeath). On the 5th mark, the count resets
		// to 0 and the account is locked out for 24 hours —
		// loadUserAndState/Login/Me all refuse the account until that
		// lockout expires, which is what actually enforces "forced
		// logout" (the JWT itself can't be revoked, so this is
		// checked on every subsequent request instead — see
		// loadUserAndState's doc comment).
		newMarks, newLockedUntil, derr := h.DB.RecordDeath(userID)
		if derr != nil {
			log.Printf("handlers: game: record death for %s: %v", userID, derr)
			return deathMarks, 0, lines, true
		}
		deathMarks = newMarks
		lockedUntilUnix = newLockedUntil

		state.InCombat = nil
		state.CurrentHP = maxHP

		if newLockedUntil > 0 {
			unlockAt := time.Unix(newLockedUntil, 0).UTC()
			lines = append(lines,
				"Death marks you for the fifth time.",
				fmt.Sprintf("You are cast out of the dungeon, locked out until %s.", unlockAt.Format(time.RFC3339)),
			)
		} else {
			lines = append(lines, fmt.Sprintf("You are defeated and stagger back to recover... (death mark %d/5)", newMarks))
		}

		return deathMarks, lockedUntilUnix, lines, false
	}

	// A Mirror Ward reflect (above) can finish the enemy off even
	// though its own attack didn't kill the player — enemyResult.Killed
	// only ever reflects the ENEMY's swing at the player, so this is
	// checked independently, after that branch, rather than folded
	// into it.
	if state.InCombat != nil && state.InCombat.EnemyCurrentHP <= 0 {
		if state.InCombat.BossPhase > 0 && state.InCombat.BossPhase < len(game.BossPhases) {
			lines = advanceBossPhase(rng, class, state, enemy, lines)
		} else {
			lines = grantVictoryRewards(rng, class, state, enemy, lines)
		}
	}

	return deathMarks, lockedUntilUnix, lines, false
}

// handleAttack resolves one full combat round: the player swings
// first, and if the enemy survives, it swings back in the same
// request. This means attack.js only ever needs to send one action
// type to both start and continue a fight — InCombat being nil is
// treated as "spawn the current encounter's enemy" rather than
// requiring a separate start-combat call.
func (h *GameHandler) handleAttack(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int, targetID string) {
	if state.AtTavern {
		writeError(w, http.StatusConflict, "you're in the tavern — leave first (tavern_leave) before fighting")
		return
	}
	if state.IsRunComplete() {
		writeError(w, http.StatusConflict, "run already complete")
		return
	}
	if state.PendingAdvance {
		writeError(w, http.StatusConflict, "current encounter already cleared — descend before attacking again")
		return
	}

	class, ok := game.GetClass(state.Class)
	if !ok {
		log.Printf("handlers: game: attack: save state for %s has unknown class %q", userID, state.Class)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	weapon, ok := game.GetWeapon(state.Equipped.WeaponID)
	if !ok {
		log.Printf("handlers: game: attack: save state for %s has unknown weapon %q", userID, state.Equipped.WeaponID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	stats, err := state.EffectiveStats()
	if err != nil {
		log.Printf("handlers: game: attack: effective stats for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	maxHP, err := state.EffectiveMaxHP()
	if err != nil {
		log.Printf("handlers: game: attack: effective max hp for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	playerProfile, err := game.BuildPlayerAttackProfile(class, weapon, stats)
	if err != nil {
		log.Printf("handlers: game: attack: build player profile for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	playerAC := game.ArmorClass(stats)

	if !h.ensureInCombat(userID, &state) {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// The Stage 10/Part 3 ambush is a simultaneous 3-enemy fight, not
	// this function's ordinary single-enemy shape — branched off
	// entirely here so every other encounter's attack resolution below
	// stays byte-for-byte what it always was. See state.go's
	// InCombat.Enemies doc comment for why these are two separate
	// fight shapes rather than one generalized over the other.
	if state.InCombat.Enemies != nil {
		h.handleAmbushAttack(w, userID, class, playerProfile, playerAC, maxHP, state, deathMarks, targetID)
		return
	}

	baseEnemy, ok := game.GetEnemy(state.InCombat.EnemyID)
	if !ok {
		log.Printf("handlers: game: attack: save state for %s has unknown enemy %q", userID, state.InCombat.EnemyID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	// Re-derived from (base enemy, state.Difficulty) on every read —
	// see ApplyDifficulty's doc comment in enemies.go for why nothing
	// caches a scaled snapshot.
	enemy := game.ApplyDifficulty(baseEnemy, state.Difficulty)
	enemyProfile, err := game.BuildEnemyAttackProfile(enemy)
	if err != nil {
		log.Printf("handlers: game: attack: build enemy profile for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	enemyAC := game.ArmorClass(enemy.Stats)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var lines []string
	var lockedUntilUnix int64 // set below only if this attack triggers the 5th death mark

	var playerResult game.AttackResult
	if state.InCombat.NextAttackGuaranteedHit {
		playerResult = game.ResolveGuaranteedAttack(rng, playerProfile, state.InCombat.EnemyCurrentHP)
		state.InCombat.NextAttackGuaranteedHit = false
		lines = append(lines, "Your steadied aim guarantees the strike lands.")
	} else {
		playerResult = game.ResolveAttack(rng, playerProfile, enemyAC, state.InCombat.EnemyCurrentHP)
	}

	// Rogue's Sneak Attack (classes.go) — passive, not gated on
	// AbilityUsedThisStage. Triggers on the first LANDED attack of a
	// fight (a miss doesn't burn the window) against an enemy still
	// at full HP, checked before EnemyCurrentHP is decremented below.
	// Stacks with, doesn't replace, whatever ConditionOneHitKillChance
	// the weapon already rolled above — if the weapon already killed
	// it, Killed is already true and this whole block is skipped.
	sneakAttackTriggered := false
	if class.ID == game.ClassRogue && playerResult.Hit && !playerResult.Killed &&
		!state.InCombat.FirstAttackLanded && state.InCombat.EnemyCurrentHP == enemy.MaxHP {
		if game.RollSneakAttack(rng) {
			playerResult.RolledOneHitKill = true
			playerResult.Killed = true
			sneakAttackTriggered = true
		}
	}
	if class.ID == game.ClassRogue && playerResult.Hit {
		state.InCombat.FirstAttackLanded = true
	}

	lines = append(lines, describeAttack("You", enemy.Name, playerResult))
	if sneakAttackTriggered {
		lines = append(lines, "You strike from the shadows — a perfect opening!")
	}

	if playerResult.Hit {
		state.InCombat.EnemyCurrentHP -= playerResult.DamageDealt
		if state.InCombat.EnemyCurrentHP < 0 {
			state.InCombat.EnemyCurrentHP = 0
		}
		if playerResult.LifestealHealAmount > 0 {
			state.CurrentHP += playerResult.LifestealHealAmount
			if state.CurrentHP > maxHP {
				state.CurrentHP = maxHP
			}
			lines = append(lines, fmt.Sprintf("You drain %d HP from the wound.", playerResult.LifestealHealAmount))
		}
		lines = applyWeaponAilments(&state, playerResult, enemy.Name, lines)
	}

	if playerResult.Killed {
		if state.InCombat.BossPhase > 0 && state.InCombat.BossPhase < len(game.BossPhases) {
			lines = advanceBossPhase(rng, class, &state, enemy, lines)
		} else {
			lines = grantVictoryRewards(rng, class, &state, enemy, lines)
		}
	} else {
		// Enemy survives the player's swing — the bonded familiar (if
		// any) gets its own mini-turn here before the enemy strikes
		// back, same "alongside the player's action" ordering as
		// resolveFamiliarMiniTurn's doc comment describes.
		var blockNextHit, familiarKilledEnemy bool
		lines, blockNextHit, familiarKilledEnemy = resolveFamiliarMiniTurn(rng, class, &state, enemy, maxHP, lines)

		if !familiarKilledEnemy {
			// Enemy survives and strikes back in the same request —
			// see the doc comment on handleAttack for why this isn't
			// a separate call.
			var fatal bool
			deathMarks, lockedUntilUnix, lines, fatal = h.resolveEnemyCounterattack(userID, class, &state, deathMarks, rng, enemy, enemyProfile, playerAC, maxHP, blockNextHit, lines)
			if fatal {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
		}
	}

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.respondWithState(w, state, deathMarks, lockedUntilUnix, lines)
}

// SecondWindHealPercent/MendHealPercent are Fighter's Second Wind and
// Cleric's Mend (classes.go) heal amounts, as a percentage of max HP —
// mirrors Arcane Mend's HealPercentOfMaxHP shape (spells.go) exactly,
// just not spell-costed since neither class has a mana economy.
// Second Wind sits above both Mend and Arcane Mend since it's a
// Fighter's ONLY sustain option all stage; Mend sits below Arcane
// Mend per its own flavor text ("smaller than Second Wind").
const (
	SecondWindHealPercent = 40
	MendHealPercent       = 15
)

// handleAbility resolves a class's signature ability action: `ability`.
// Only Fighter (Second Wind), Cleric (Mend), and Ranger (Steady Aim)
// are usable through this action — Mage's Firebolt is a spell (cast
// via "cast", not this), and Rogue's Sneak Attack is passive (see
// handleAttack/handleAmbushAttack's FirstAttackLanded checks), so
// both of those classes get a clear rejection here rather than a
// silent no-op.
func (h *GameHandler) handleAbility(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int) {
	if state.AtTavern {
		writeError(w, http.StatusConflict, "you're in the tavern — leave first (tavern_leave) before fighting")
		return
	}
	if state.IsRunComplete() {
		writeError(w, http.StatusConflict, "run already complete")
		return
	}
	if state.PendingAdvance {
		writeError(w, http.StatusConflict, "current encounter already cleared — descend before acting again")
		return
	}

	class, ok := game.GetClass(state.Class)
	if !ok {
		log.Printf("handlers: game: ability: save state for %s has unknown class %q", userID, state.Class)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if class.ID != game.ClassFighter && class.ID != game.ClassCleric && class.ID != game.ClassRanger {
		writeError(w, http.StatusForbidden, fmt.Sprintf("%s has no usable ability action (Mage: use \"cast\"; Rogue's Sneak Attack triggers automatically on your first landed hit)", class.Name))
		return
	}
	if state.AbilityUsedThisStage {
		writeError(w, http.StatusConflict, fmt.Sprintf("you've already used %s this stage", class.AbilityName))
		return
	}

	stats, err := state.EffectiveStats()
	if err != nil {
		log.Printf("handlers: game: ability: effective stats for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	maxHP, err := state.EffectiveMaxHP()
	if err != nil {
		log.Printf("handlers: game: ability: effective max hp for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	playerAC := game.ArmorClass(stats)

	if !h.ensureInCombat(userID, &state) {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var lines []string
	var lockedUntilUnix int64

	// The ability is spent on the attempt itself, unconditionally,
	// before resolving its effect — mirrors handleCast's Mana
	// deduction happening before the switch on spell.Kind.
	state.AbilityUsedThisStage = true

	switch class.ID {
	case game.ClassFighter:
		healAmount := (maxHP * SecondWindHealPercent) / 100
		state.CurrentHP += healAmount
		if state.CurrentHP > maxHP {
			state.CurrentHP = maxHP
		}
		lines = append(lines, fmt.Sprintf("You use %s and recover %d HP.", class.AbilityName, healAmount))
	case game.ClassCleric:
		healAmount := (maxHP * MendHealPercent) / 100
		state.CurrentHP += healAmount
		if state.CurrentHP > maxHP {
			state.CurrentHP = maxHP
		}
		lines = append(lines, fmt.Sprintf("You use %s and recover %d HP.", class.AbilityName, healAmount))
	case game.ClassRanger:
		state.InCombat.NextAttackGuaranteedHit = true
		lines = append(lines, fmt.Sprintf("You use %s, steadying your aim for your next attack.", class.AbilityName))
	}

	// Using the ability spends the turn exactly like a non-killing
	// attack or Arcane Mend cast would — the familiar still gets its
	// mini-turn, and the enemy (or all three ambush enemies) still
	// strike back.
	if state.InCombat.Enemies != nil {
		var fatal bool
		deathMarks, lockedUntilUnix, lines, fatal = h.resolveAmbushRound(userID, class, &state, deathMarks, rng, playerAC, maxHP, -1, lines)
		if fatal {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
	} else {
		baseEnemy, ok := game.GetEnemy(state.InCombat.EnemyID)
		if !ok {
			log.Printf("handlers: game: ability: save state for %s has unknown enemy %q", userID, state.InCombat.EnemyID)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		enemy := game.ApplyDifficulty(baseEnemy, state.Difficulty)
		enemyProfile, err := game.BuildEnemyAttackProfile(enemy)
		if err != nil {
			log.Printf("handlers: game: ability: build enemy profile for %s: %v", userID, err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}

		var blockNextHit, familiarKilledEnemy bool
		lines, blockNextHit, familiarKilledEnemy = resolveFamiliarMiniTurn(rng, class, &state, enemy, maxHP, lines)

		if !familiarKilledEnemy {
			var fatal bool
			deathMarks, lockedUntilUnix, lines, fatal = h.resolveEnemyCounterattack(userID, class, &state, deathMarks, rng, enemy, enemyProfile, playerAC, maxHP, blockNextHit, lines)
			if fatal {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
		}
	}

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.respondWithState(w, state, deathMarks, lockedUntilUnix, lines)
}

// handleAmbushAttack resolves one full round of the player attacking a
// specific enemy in the Stage 10/Part 3 simultaneous ambush — the
// weapon-swing counterpart to handleAttack's ordinary single-enemy
// round, called from handleAttack the moment state.InCombat.Enemies is
// found to be non-nil. Player/weapon/stats setup is already done by
// the caller (identical for either fight shape); this just resolves
// the player's swing against ONE targeted enemy, then hands off to
// resolveAmbushRound for the familiar mini-turn, every surviving
// enemy's counterattack, and the victory check.
func (h *GameHandler) handleAmbushAttack(
	w http.ResponseWriter,
	userID string,
	class game.Class,
	playerProfile game.AttackProfile,
	playerAC int,
	maxHP int,
	state game.SaveState,
	deathMarks int,
	targetID string,
) {
	targetIdx, errMsg := resolveAmbushTarget(&state, targetID)
	if errMsg != "" {
		writeError(w, http.StatusBadRequest, errMsg)
		return
	}

	baseEnemy, ok := game.GetEnemy(state.InCombat.Enemies[targetIdx].ID)
	if !ok {
		log.Printf("handlers: game: ambush attack: save state for %s has unknown ambush enemy %q", userID, state.InCombat.Enemies[targetIdx].ID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	enemy := game.ApplyDifficulty(baseEnemy, state.Difficulty)
	enemyAC := game.ArmorClass(enemy.Stats)

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var lines []string
	var lockedUntilUnix int64

	ce := &state.InCombat.Enemies[targetIdx]

	var playerResult game.AttackResult
	if state.InCombat.NextAttackGuaranteedHit {
		playerResult = game.ResolveGuaranteedAttack(rng, playerProfile, ce.CurrentHP)
		state.InCombat.NextAttackGuaranteedHit = false
		lines = append(lines, "Your steadied aim guarantees the strike lands.")
	} else {
		playerResult = game.ResolveAttack(rng, playerProfile, enemyAC, ce.CurrentHP)
	}

	// Same Sneak Attack rule as handleAttack's single-enemy path,
	// checked against the SPECIFIC ambush enemy just targeted.
	// FirstAttackLanded is fight-wide (not per-enemy) — the window is
	// "your first landed swing of this whole ambush," not "your first
	// swing against each individual enemy."
	sneakAttackTriggered := false
	if class.ID == game.ClassRogue && playerResult.Hit && !playerResult.Killed &&
		!state.InCombat.FirstAttackLanded && ce.CurrentHP == enemy.MaxHP {
		if game.RollSneakAttack(rng) {
			playerResult.RolledOneHitKill = true
			playerResult.Killed = true
			sneakAttackTriggered = true
		}
	}
	if class.ID == game.ClassRogue && playerResult.Hit {
		state.InCombat.FirstAttackLanded = true
	}

	lines = append(lines, describeAttack("You", enemy.Name, playerResult))
	if sneakAttackTriggered {
		lines = append(lines, "You strike from the shadows — a perfect opening!")
	}

	if playerResult.Hit {
		ce.CurrentHP -= playerResult.DamageDealt
		if ce.CurrentHP < 0 {
			ce.CurrentHP = 0
		}
		if playerResult.LifestealHealAmount > 0 {
			state.CurrentHP += playerResult.LifestealHealAmount
			if state.CurrentHP > maxHP {
				state.CurrentHP = maxHP
			}
			lines = append(lines, fmt.Sprintf("You drain %d HP from the wound.", playerResult.LifestealHealAmount))
		}
		lines = applyAmbushWeaponAilments(ce, playerResult, enemy.Name, lines)
	}

	if playerResult.Killed {
		ce.CurrentHP = 0
		lines = append(lines, fmt.Sprintf("%s is defeated!", enemy.Name))
	}

	var fatal bool
	deathMarks, lockedUntilUnix, lines, fatal = h.resolveAmbushRound(userID, class, &state, deathMarks, rng, playerAC, maxHP, targetIdx, lines)
	if fatal {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.respondWithState(w, state, deathMarks, lockedUntilUnix, lines)
}

// handleCast resolves one spell cast: `cast <spell_id>`. Mage-only —
// see game.MageSpells' doc comment on spells being a permanent,
// always-known list gated purely by Mana, not a stage-based
// once-per-encounter limiter the way the other classes' flavor
// abilities are described (see classes.go). Mirrors handleAttack's
// "may both spawn AND resolve a full round in one request" shape so
// cast.js only ever needs to send one request type too — casting a
// spell spends the player's turn exactly like a weapon attack does,
// including the enemy's counterswing if it survives.
func (h *GameHandler) handleCast(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int, spellID string, targetID string) {
	if state.AtTavern {
		writeError(w, http.StatusConflict, "you're in the tavern — leave first (tavern_leave) before fighting")
		return
	}
	if state.IsRunComplete() {
		writeError(w, http.StatusConflict, "run already complete")
		return
	}
	if state.PendingAdvance {
		writeError(w, http.StatusConflict, "current encounter already cleared — descend before acting again")
		return
	}

	class, ok := game.GetClass(state.Class)
	if !ok {
		log.Printf("handlers: game: cast: save state for %s has unknown class %q", userID, state.Class)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if class.ID != game.ClassMage {
		writeError(w, http.StatusForbidden, "only a Mage can cast spells")
		return
	}

	// GetKnownSpell checks BOTH the fixed MageSpells starting kit and
	// any tavern-bought scrolls this specific character has learned
	// (state.LearnedSpells) — a plain game.GetSpell here would let a
	// Mage cast a scroll spell whose ID is valid content but that they
	// never actually paid for.
	spell, ok := game.GetKnownSpell(state, spellID)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown spell_id")
		return
	}
	if state.Mana < spell.ManaCost {
		writeError(w, http.StatusConflict, fmt.Sprintf("not enough mana to cast %s (need %d, have %d)", spell.Name, spell.ManaCost, state.Mana))
		return
	}

	stats, err := state.EffectiveStats()
	if err != nil {
		log.Printf("handlers: game: cast: effective stats for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	maxHP, err := state.EffectiveMaxHP()
	if err != nil {
		log.Printf("handlers: game: cast: effective max hp for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	playerAC := game.ArmorClass(stats)

	if !h.ensureInCombat(userID, &state) {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	// See handleAttack's identical branch — the ambush is a separate
	// fight shape entirely, handled by its own function so the
	// single-enemy/boss code below is untouched.
	if state.InCombat.Enemies != nil {
		h.handleAmbushCast(w, userID, class, state, deathMarks, spell, stats, maxHP, playerAC, targetID)
		return
	}

	baseEnemy, ok := game.GetEnemy(state.InCombat.EnemyID)
	if !ok {
		log.Printf("handlers: game: cast: save state for %s has unknown enemy %q", userID, state.InCombat.EnemyID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	enemy := game.ApplyDifficulty(baseEnemy, state.Difficulty)
	enemyProfile, err := game.BuildEnemyAttackProfile(enemy)
	if err != nil {
		log.Printf("handlers: game: cast: build enemy profile for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var lines []string
	var lockedUntilUnix int64
	var bossPhaseTransitioned bool // see its use below, right after the switch

	// Mana is spent on the attempt itself, not gated on whether the
	// spell lands — mirrors a real d20-style spell slot: you don't get
	// it back for a missed Chain Lightning any more than a Fighter
	// gets a swing back for a missed attack.
	state.Mana -= spell.ManaCost

	switch spell.Kind {
	case game.SpellKindHeal:
		healAmount := (maxHP * spell.HealPercentOfMaxHP) / 100
		state.CurrentHP += healAmount
		if state.CurrentHP > maxHP {
			state.CurrentHP = maxHP
		}
		lines = append(lines, fmt.Sprintf("You cast %s and recover %d HP.", spell.Name, healAmount))

	case game.SpellKindDamage:
		var result game.AttackResult
		if spell.AutoHit {
			statMod := statModifierFor(class.AttackStat, stats)
			damage := game.ResolveAutoHitSpellDamage(rng, spell, statMod)
			result = game.AttackResult{Hit: true, DamageDealt: damage}
			if damage >= state.InCombat.EnemyCurrentHP {
				result.Killed = true
			}
			lines = append(lines, fmt.Sprintf("You cast %s — it strikes true for %d damage.", spell.Name, damage))
		} else {
			statMod := statModifierFor(class.AttackStat, stats)
			profile := game.AttackProfile{
				AttackStatModifier: statMod,
				DamageStatModifier: statMod,
				DamageDieSides:     spell.DamageDieSides,
				DamageDieCount:     spell.DamageDieCount,
			}
			result = game.ResolveAttack(rng, profile, game.ArmorClass(enemy.Stats), state.InCombat.EnemyCurrentHP)

			// A bonded Stormtail familiar resonates with Chain Lightning
			// specifically — its own kit is lightning-flavored (see
			// FamiliarKindStormtail's stun chance) — boosting the
			// spell's resolved damage by 30%. Applied to the roll's
			// OUTPUT rather than padding DamageDieCount/Sides
			// beforehand, so the bonus is an exact +30% of whatever
			// was actually rolled, not its own separate die.
			stormtailBoosted := result.Hit && spell.ID == "s_chain_lightning" && hasFamiliar(state, game.FamiliarKindStormtail)
			if stormtailBoosted {
				result.DamageDealt = (result.DamageDealt * 13) / 10
				if result.DamageDealt >= state.InCombat.EnemyCurrentHP {
					result.Killed = true
				}
			}

			lines = append(lines, describeSpellAttack(spell.Name, enemy.Name, result))
			if stormtailBoosted {
				lines = append(lines, "Your Stormtail's static charge surges through the bolt, empowering it!")
			}
		}

		if result.Hit {
			state.InCombat.EnemyCurrentHP -= result.DamageDealt
			if state.InCombat.EnemyCurrentHP < 0 {
				state.InCombat.EnemyCurrentHP = 0
			}
			// Spells don't currently carry a SpecialCondition (neither
			// AttackProfile above nor the AutoHit branch sets one), so
			// this is a no-op today — wired anyway so a future
			// elemental-scroll spell that DOES set one works for free,
			// same shared path handleAttack's weapon swing uses.
			lines = applyWeaponAilments(&state, result, enemy.Name, lines)
		}

		if result.Killed {
			if state.InCombat.BossPhase > 0 && state.InCombat.BossPhase < len(game.BossPhases) {
				lines = advanceBossPhase(rng, class, &state, enemy, lines)
				bossPhaseTransitioned = true
			} else {
				lines = grantVictoryRewards(rng, class, &state, enemy, lines)
			}
		}
	}

	// bossPhaseTransitioned guards against a stale counterattack: after
	// advanceBossPhase, state.InCombat is non-nil again (it now holds
	// the FRESH next phase), but `enemy`/`enemyProfile` here still
	// describe the phase that just died — resolveEnemyCounterattack
	// would otherwise resolve a counterswing using the wrong phase's
	// stats. A newly-spawned phase doesn't get a free hit the instant
	// it appears, same as ensureInCombat's normal spawn not attacking
	// before the player's first move — it waits for the player's next
	// action instead.
	if state.InCombat != nil && !bossPhaseTransitioned {
		// The spell didn't end the fight — casting still spends the
		// player's turn, so the bonded familiar gets its mini-turn
		// and the enemy strikes back exactly as it would after a
		// non-killing weapon attack.
		var blockNextHit, familiarKilledEnemy bool
		lines, blockNextHit, familiarKilledEnemy = resolveFamiliarMiniTurn(rng, class, &state, enemy, maxHP, lines)

		if !familiarKilledEnemy {
			var fatal bool
			deathMarks, lockedUntilUnix, lines, fatal = h.resolveEnemyCounterattack(userID, class, &state, deathMarks, rng, enemy, enemyProfile, playerAC, maxHP, blockNextHit, lines)
			if fatal {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
		}
	}

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.respondWithState(w, state, deathMarks, lockedUntilUnix, lines)
}

// handleAmbushCast resolves one spell cast during the Stage 10/Part 3
// simultaneous ambush — handleCast's ambush counterpart, called the
// moment state.InCombat.Enemies is found non-nil. Mirrors handleCast's
// two spell-kind branches exactly (heal, damage) but a damage spell
// resolves against ONE targeted living enemy instead of the single
// InCombat.EnemyCurrentHP, and hands off to the shared
// resolveAmbushRound afterward instead of resolveEnemyCounterattack.
func (h *GameHandler) handleAmbushCast(
	w http.ResponseWriter,
	userID string,
	class game.Class,
	state game.SaveState,
	deathMarks int,
	spell game.Spell,
	stats game.Stats,
	maxHP int,
	playerAC int,
	targetID string,
) {
	// A heal has no enemy target of its own — resolveAmbushTarget is
	// only actually load-bearing for SpellKindDamage below, but it's
	// resolved unconditionally here so a heal still hands
	// resolveAmbushRound a sensible familiar-mini-turn target (the
	// lowest-HP living enemy) rather than an unresolved index.
	targetIdx, errMsg := resolveAmbushTarget(&state, targetID)
	if errMsg != "" {
		if spell.Kind == game.SpellKindDamage {
			writeError(w, http.StatusBadRequest, errMsg)
			return
		}
		// A heal doesn't actually need a target — a bad/unresolved
		// targetID only matters for the familiar mini-turn's fallback
		// below, so fall back to the lowest-HP living enemy instead of
		// carrying forward whatever bogus index resolveAmbushTarget's
		// error path returned.
		if idx, ok := lowestHPLivingAmbushEnemy(&state); ok {
			targetIdx = idx
		}
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var lines []string
	var lockedUntilUnix int64

	state.Mana -= spell.ManaCost

	switch spell.Kind {
	case game.SpellKindHeal:
		healAmount := (maxHP * spell.HealPercentOfMaxHP) / 100
		state.CurrentHP += healAmount
		if state.CurrentHP > maxHP {
			state.CurrentHP = maxHP
		}
		lines = append(lines, fmt.Sprintf("You cast %s and recover %d HP.", spell.Name, healAmount))

	case game.SpellKindDamage:
		baseEnemy, ok := game.GetEnemy(state.InCombat.Enemies[targetIdx].ID)
		if !ok {
			log.Printf("handlers: game: ambush cast: save state for %s has unknown ambush enemy %q", userID, state.InCombat.Enemies[targetIdx].ID)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		enemy := game.ApplyDifficulty(baseEnemy, state.Difficulty)
		ce := &state.InCombat.Enemies[targetIdx]

		var result game.AttackResult
		if spell.AutoHit {
			statMod := statModifierFor(class.AttackStat, stats)
			damage := game.ResolveAutoHitSpellDamage(rng, spell, statMod)
			result = game.AttackResult{Hit: true, DamageDealt: damage}
			if damage >= ce.CurrentHP {
				result.Killed = true
			}
			lines = append(lines, fmt.Sprintf("You cast %s — it strikes true for %d damage.", spell.Name, damage))
		} else {
			statMod := statModifierFor(class.AttackStat, stats)
			profile := game.AttackProfile{
				AttackStatModifier: statMod,
				DamageStatModifier: statMod,
				DamageDieSides:     spell.DamageDieSides,
				DamageDieCount:     spell.DamageDieCount,
			}
			result = game.ResolveAttack(rng, profile, game.ArmorClass(enemy.Stats), ce.CurrentHP)

			stormtailBoosted := result.Hit && spell.ID == "s_chain_lightning" && hasFamiliar(state, game.FamiliarKindStormtail)
			if stormtailBoosted {
				result.DamageDealt = (result.DamageDealt * 13) / 10
				if result.DamageDealt >= ce.CurrentHP {
					result.Killed = true
				}
			}

			lines = append(lines, describeSpellAttack(spell.Name, enemy.Name, result))
			if stormtailBoosted {
				lines = append(lines, "Your Stormtail's static charge surges through the bolt, empowering it!")
			}
		}

		if result.Hit {
			ce.CurrentHP -= result.DamageDealt
			if ce.CurrentHP < 0 {
				ce.CurrentHP = 0
			}
			lines = applyAmbushWeaponAilments(ce, result, enemy.Name, lines)
		}

		if result.Killed {
			ce.CurrentHP = 0
			lines = append(lines, fmt.Sprintf("%s is defeated!", enemy.Name))
		}
	}

	var fatal bool
	deathMarks, lockedUntilUnix, lines, fatal = h.resolveAmbushRound(userID, class, &state, deathMarks, rng, playerAC, maxHP, targetIdx, lines)
	if fatal {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.respondWithState(w, state, deathMarks, lockedUntilUnix, lines)
}

// handleUse consumes a potion from Inventory: `use <item_id>`.
// Potions are permanent, one-time effects (see items.go's PotionKind
// doc comment) resolved entirely here, mirroring handleEquip's role
// as the one place an Inventory item actually gets consumed/applied —
// unlike equip, a used potion never goes back into Inventory.
func (h *GameHandler) handleUse(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int, itemID string) {
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "item_id is required for use")
		return
	}
	potion, ok := game.GetPotion(itemID)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown potion item_id")
		return
	}
	held := false
	removed := false
	remaining := state.Inventory[:0:0]
	for _, id := range state.Inventory {
		if id == itemID && !removed {
			held = true
			removed = true
			continue
		}
		remaining = append(remaining, id)
	}
	if !held {
		writeError(w, http.StatusBadRequest, "item is not in your inventory")
		return
	}
	state.Inventory = remaining
	var lines []string
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	switch potion.Kind {
	case game.PotionKindHP:
		maxHP, err := state.EffectiveMaxHP()
		if err != nil {
			log.Printf("handlers: game: use: effective max hp for %s: %v", userID, err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		state.CurrentHP = maxHP
		lines = append(lines, fmt.Sprintf("You drink the %s and are restored to full HP.", potion.Name))
	case game.PotionKindStat:
		// Random stat, random magnitude (3-5 inclusive) — a per-use
		// roll, not fixed potion data, see PotionKindStat's doc
		// comment in items.go.
		amount := 3 + rng.Intn(3)
		statName, bonusTotal := applyRandomStatBonus(&state, rng, amount)
		lines = append(lines, fmt.Sprintf(
			"You drink the %s. Your %s permanently increases by %d (permanent bonus now +%d).",
			potion.Name, statName, amount, bonusTotal,
		))
	default:
		log.Printf("handlers: game: use: unknown potion kind %q for %s", potion.Kind, userID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.respondWithState(w, state, deathMarks, 0, lines)
}

// applyRandomStatBonus rolls which of STR/DEX/CON a stat potion
// boosts and applies it directly to state.StatBonuses, permanently —
// StatBonuses persists across gear changes and runs indefinitely
// (see SaveState.StatBonuses' doc comment), unlike an equipped item's
// stat mod. Split out from handleUse only so the roll-and-apply step
// has a single well-named place, matching this file's existing
// small-helper style (describeAttack, lockoutMessage).
func applyRandomStatBonus(state *game.SaveState, rng *rand.Rand, amount int) (statName string, bonusTotal int) {
	switch rng.Intn(3) {
	case 0:
		state.StatBonuses.STR += amount
		return "STR", state.StatBonuses.STR
	case 1:
		state.StatBonuses.DEX += amount
		return "DEX", state.StatBonuses.DEX
	default:
		state.StatBonuses.CON += amount
		return "CON", state.StatBonuses.CON
	}
}

// handleEquip swaps in an armor piece from Inventory. Weapons are
// never equip-able post-creation — see items.go's design note on
// weapons being permanently class-restricted — so any item_id that
// isn't a valid armor ID is rejected outright, not just "not found."
func (h *GameHandler) handleEquip(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int, itemID string) {
	if state.InCombat != nil {
		writeError(w, http.StatusConflict, "cannot change equipment mid-combat")
		return
	}
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "item_id is required for equip")
		return
	}

	armor, ok := game.GetArmor(itemID)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown armor item_id")
		return
	}

	held := false
	remaining := state.Inventory[:0:0]
	for _, id := range state.Inventory {
		if id == itemID {
			held = true
			continue
		}
		remaining = append(remaining, id)
	}
	if !held {
		writeError(w, http.StatusBadRequest, "item is not in your inventory")
		return
	}

	// The previously-equipped piece (if any) goes back into inventory
	// rather than being discarded — equipping is a swap, not a
	// one-way consumption.
	if state.Equipped.ArmorID != "" {
		remaining = append(remaining, state.Equipped.ArmorID)
	}
	state.Inventory = remaining
	state.Equipped.ArmorID = armor.ID

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.respondWithState(w, state, deathMarks, 0, []string{fmt.Sprintf("You equip the %s.", armor.Name)})
}

// handleDescend advances past a cleared encounter, granting its armor
// reward (if any) first. Requires PendingAdvance — see state.go's doc
// comment on that field for why this can't just check InCombat.
func (h *GameHandler) handleDescend(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int) {
	if state.AtTavern {
		writeError(w, http.StatusConflict, "you're in the tavern — type tavern_leave to head back down")
		return
	}
	if state.InCombat != nil {
		writeError(w, http.StatusConflict, "cannot descend mid-combat")
		return
	}
	if !state.PendingAdvance {
		writeError(w, http.StatusConflict, "clear the current encounter before descending")
		return
	}

	lines := []string{}

	// Captured BEFORE AdvancePart() mutates CurrentStage/CurrentPart —
	// "did we just clear encounter X" can only be answered by looking
	// at where the player WAS, not where AdvancePart is about to move
	// them to.
	clearingStage2Finale := state.CurrentStage == 2 && state.CurrentPart == 3
	clearingDungeonFinale := state.CurrentStage == game.DungeonFinaleStage && state.CurrentPart == game.DungeonFinalePart
	clearingAncientWoodsFinale := state.CurrentStage == game.AncientWoodsFamiliarStage && state.CurrentPart == game.AncientWoodsFamiliarPart

	if clearedEncounter, ok := game.GetEncounter(state.CurrentStage, state.CurrentPart); ok {
		if clearedEncounter.RewardArmorID != "" {
			state.Inventory = append(state.Inventory, clearedEncounter.RewardArmorID)
			if armor, ok := game.GetArmor(clearedEncounter.RewardArmorID); ok {
				lines = append(lines, fmt.Sprintf("You claim the %s.", armor.Name))
			}
		}
		if clearedEncounter.RewardPotionID != "" {
			state.Inventory = append(state.Inventory, clearedEncounter.RewardPotionID)
			if potion, ok := game.GetPotion(clearedEncounter.RewardPotionID); ok {
				lines = append(lines, fmt.Sprintf("You claim a %s.", potion.Name))
			}
		}
	}

	// The second familiar is a guaranteed grant tied to a specific
	// encounter, not a combat-victory reward — resolved here, right
	// alongside the encounter's ordinary armor/potion rewards above,
	// rather than in grantVictoryRewards (which only ever runs for
	// the enemy that was JUST killed, not the encounter being
	// descended past).
	if clearingAncientWoodsFinale {
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		famID, wasPrimary := game.GrantSecondFamiliar(rng, &state)
		if famID != "" {
			if familiar, ok := game.GetFamiliar(famID); ok {
				if wasPrimary {
					lines = append(lines, familiar.DisplayFlavor)
				} else {
					lines = append(lines, fmt.Sprintf("As you leave the Ancient Woods, one final spirit emerges. It has watched your courage — %s has chosen to accompany you too.", familiar.Name))
				}
			}
		}
	}

	state.AdvancePart()
	state.PendingAdvance = false

	if state.IsRunComplete() {
		// By the time the run can complete at all, DungeonComplete is
		// already true (clearingDungeonFinale below set it, several
		// descends ago) — this is the Journey's Black Mire finale,
		// not the dungeon's, so the narration is the kingdom-arrival
		// beat rather than "the dungeon falls silent."
		lines = append(lines,
			"The fog begins to thin. Fresh air replaces the stench of the marsh.",
			"Far ahead, a familiar silhouette rises against the sky — castle walls. Home.",
			"For the first time since entering the dungeon, you know you are going to survive.",
		)
		// The true end of a run is an audience with the king, not
		// another tavern — AtKingsChambers is set true here and never
		// cleared again for this character (there is no
		// handleChambersLeave; the only actions available from here
		// on are legacy_hall/choose_path, see below).
		state.AtKingsChambers = true
		lines = append(lines,
			"Word of your return travels faster than you do. By the time you reach the gates, a royal guard is already waiting to lead you inside.",
			`You are brought before the king. Type "hall" to enter his chambers.`,
		)
	} else if clearingStage2Finale {
		// One-time waypoint: clearing Stage 2's finale routes the
		// player into the tavern instead of straight into Stage 3.
		// tavern_leave (handleTavernLeave) is what lets them continue
		// on afterward.
		state.AtTavern = true
		state.CurrentTavernSpells = game.RollTavernSpells(rand.New(rand.NewSource(time.Now().UnixNano())))
		lines = append(lines, "Descending further, you find a torchlit tavern built into the dungeon wall — a place to rest before Stage 3.")
		if !state.ShamanBlessingReceived {
			state.StatBonuses.STR += 5
			state.StatBonuses.DEX += 5
			state.StatBonuses.CON += 5
			state.ShamanBlessingReceived = true
			lines = append(lines, "A shaman by the fire presses a hand to your chest. You feel permanently stronger, quicker, and tougher — +5 STR, +5 DEX, +5 CON.")
		}
	} else if clearingDungeonFinale {
		// Second one-time waypoint: clearing Stage 5's finale (the
		// final boss) routes the player back to the tavern instead of
		// straight into the Journey's Stage 6. Unlike the Stage 2
		// waypoint, tavern_leave alone won't move this player forward
		// — handleExitDungeon (`tavern exit`) is the only way out,
		// and it refuses until DungeonComplete is true (set right
		// here).
		state.DungeonComplete = true
		state.AtTavern = true
		state.CurrentTavernSpells = game.RollTavernSpells(rand.New(rand.NewSource(time.Now().UnixNano())))
		lines = append(lines,
			"The dungeon falls silent behind you. For now, at least, it's over.",
			"You climb back to the tavern to catch your breath before the road home.",
			`When you're ready, type "tavern exit" to begin the journey back to the kingdom.`,
		)
	}

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.respondWithState(w, state, deathMarks, 0, lines)
}

// handleExitDungeon leaves the post-dungeon tavern waypoint and begins
// the Journey (Stage 6+): `tavern exit`. Distinct from tavern_leave
// (which just clears AtTavern unconditionally) because this ALSO
// requires state.DungeonComplete — asking to exit the dungeon before
// the final boss is actually beaten should read as "you still have
// unfinished business," not silently do nothing or be indistinguishable
// from a plain tavern_leave.
func (h *GameHandler) handleExitDungeon(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int) {
	if !state.AtTavern {
		writeError(w, http.StatusConflict, "you're not in the tavern")
		return
	}
	if !state.DungeonComplete {
		writeError(w, http.StatusConflict, "you still have unfinished business")
		return
	}
	if state.IsRunComplete() {
		writeError(w, http.StatusConflict, "your journey is already complete — the tavern is the only place left for you now")
		return
	}

	state.AtTavern = false

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.respondWithState(w, state, deathMarks, 0, []string{
		"You step out of the dungeon's tavern and onto the road. The world outside is wider — and stranger — than you remember.",
	})
}

// ---------------------------------------------------------------------
// King's chambers actions (see game/legacy.go) — the true end of a
// run, reached only once state.AtKingsChambers is true (handleDescend
// sets it exactly once, on the Journey's Black Mire finale). No
// "leave" action exists for this location — see AtKingsChambers' doc
// comment in state.go.
// ---------------------------------------------------------------------

// handleLegacyHall shows the king's chambers: `hall`. Before a path is
// chosen, this is both the entry point (first call) and a harmless
// re-display (idempotent, like handleTavernLore) — it always shows
// the Hall of Legacies and the three paths on offer. Once a path HAS
// been chosen, it instead just confirms which one, rather than
// re-running the full ceremony on every repeat call.
func (h *GameHandler) handleLegacyHall(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int) {
	if !state.AtKingsChambers {
		writeError(w, http.StatusConflict, "there is no such place here")
		return
	}

	if state.LegacyPath != "" {
		lines := []string{"You have already chosen your path."}
		if path, ok := game.GetLegacyPath(state.LegacyPath); ok {
			lines = append(lines, fmt.Sprintf("Your legacy: %s.", path.Name))
		}
		h.respondWithState(w, state, deathMarks, 0, lines)
		return
	}

	legacies, err := h.DB.GetRecentLegacies()
	if err != nil {
		log.Printf("handlers: game: legacy_hall: get recent legacies for %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	lines := []string{
		"The king rises to meet you personally — word of the road you walked reached the castle well before you did.",
		"Behind him, a long tapestry lists those who stood where you're standing now, and the paths they chose.",
	}

	if len(legacies) == 0 {
		lines = append(lines, "The tapestry is blank. You are the first name it will ever hold.")
	} else {
		lines = append(lines, "Among the names already woven into it:")
		for _, l := range legacies {
			pathName := l.Path
			if path, ok := game.GetLegacyPath(l.Path); ok {
				pathName = path.Name
			}
			lines = append(lines, fmt.Sprintf("  %s the %s (%s) chose %s.", l.CharacterName, l.Class, l.Username, pathName))
		}
	}

	lines = append(lines, "", "The king asks what you intend to do with what comes next. Three paths are open to you:")
	for _, path := range game.LegacyPaths {
		lines = append(lines, fmt.Sprintf("  %s (%s) — %s", path.ID, path.Name, path.Pitch))
	}
	lines = append(lines, `Type "hall choose <path>" to decide. This choice is permanent.`)

	h.respondWithState(w, state, deathMarks, 0, lines)
}

// handleChoosePath commits a character to one of game.LegacyPaths:
// `choose <path>`. Permanent — refuses outright if state.LegacyPath is
// already set, same "refuses a second call rather than silently
// overwriting" guard familiars.go's GrantSecondFamiliar uses for an
// already-full secondary slot. On success this is also the ONE place
// a Hall of Legacies row is ever inserted (database.InsertLegacy) —
// every future completed run's `hall` call will see this character
// listed from here on.
func (h *GameHandler) handleChoosePath(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int, pathID string) {
	if !state.AtKingsChambers {
		writeError(w, http.StatusConflict, "there is no such place here")
		return
	}
	if state.LegacyPath != "" {
		writeError(w, http.StatusConflict, "you have already chosen your path")
		return
	}
	if pathID == "" {
		writeError(w, http.StatusBadRequest, "item_id is required for choose_path")
		return
	}

	path, ok := game.GetLegacyPath(pathID)
	if !ok {
		writeError(w, http.StatusBadRequest, "unknown path — choose lords, commons, or heroes")
		return
	}

	state.LegacyPath = string(path.ID)

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	user, err := h.DB.GetUserByUserID(userID)
	if err != nil {
		log.Printf("handlers: game: choose_path: get user %s: %v", userID, err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	if err := h.DB.InsertLegacy(user.Username, state.CharacterName, string(state.Class), string(path.ID)); err != nil {
		// The character's own SaveState is already saved above — a
		// failure here means their run is safely complete but they
		// won't appear in the shared Hall for others yet, not that
		// their choice was lost. Logged, not surfaced as a 500 that
		// would make the player think their choice didn't take.
		log.Printf("handlers: game: choose_path: insert legacy for %s: %v", userID, err)
	}

	lines := []string{
		path.ResultText,
		"",
		fmt.Sprintf("Your name is woven into the tapestry beside the others: %s the %s, %s.", state.CharacterName, state.Class, path.Name),
		"Your legacy is sealed. The game is over.",
	}

	h.respondWithState(w, state, deathMarks, 0, lines)
}

// ---------------------------------------------------------------------
// Tavern actions (see game/tavern.go)
// ---------------------------------------------------------------------
// All four require state.AtTavern — a player has to actually be
// standing in the tavern (handleDescend is the only place that ever
// sets it true) to use any of its services, same "you have to be
// somewhere to interact with it" gate handleAttack/handleCast apply
// to combat.

// handleTavernLore sells the one-time monster-effectiveness lore
// unlock: `tavern_lore`. Idempotent — calling it again once already
// learned is a harmless no-op, not an error, mirroring
// SetEasterEggFound's idempotence convention on the portfolio side.
func (h *GameHandler) handleTavernLore(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int) {
	if !state.AtTavern {
		writeError(w, http.StatusConflict, "you need to be in the tavern to do that")
		return
	}

	var lines []string
	if state.HasLearnedMonsterLore {
		lines = append(lines, "You already know this — the tavern-keeper has nothing new to add.")
	} else {
		state.HasLearnedMonsterLore = true
		lines = append(lines, "The tavern-keeper leans in and shares what they know:")
		lines = append(lines, game.MonsterLore...)
	}

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.respondWithState(w, state, deathMarks, 0, lines)
}

// handleTavernBuy purchases a tonic or a spell scroll: `tavern_buy
// <item_id>`. itemID is looked up against BOTH game.TavernPotions and
// game.ScrollPrices (in that order) — the two are disjoint ID spaces
// (potion IDs vs. spell IDs), so there's no ambiguity in checking one
// then the other rather than requiring the caller to specify which
// kind of item they mean.
func (h *GameHandler) handleTavernBuy(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int, itemID string) {
	if !state.AtTavern {
		writeError(w, http.StatusConflict, "you need to be in the tavern to do that")
		return
	}
	if itemID == "" {
		writeError(w, http.StatusBadRequest, "item_id is required for tavern_buy")
		return
	}

	if price, ok := game.TavernPotions[itemID]; ok {
		if state.Gold < price {
			writeError(w, http.StatusConflict, fmt.Sprintf("not enough gold (need %d, have %d)", price, state.Gold))
			return
		}
		potion, ok := game.GetPotion(itemID)
		if !ok {
			log.Printf("handlers: game: tavern_buy: TavernPotions references unknown potion %q", itemID)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		state.Gold -= price
		state.Inventory = append(state.Inventory, itemID)

		if err := h.saveState(userID, &state); err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		h.respondWithState(w, state, deathMarks, 0, []string{fmt.Sprintf("You buy a %s for %d gold.", potion.Name, price)})
		return
	}

	if price, ok := game.ScrollPrices[itemID]; ok {
		if state.Class != game.ClassMage {
			writeError(w, http.StatusForbidden, "only a Mage can learn a spell from a scroll")
			return
		}
		offered := false
		for _, offeredID := range state.CurrentTavernSpells {
			if offeredID == itemID {
				offered = true
				break
			}
		}
		if !offered {
			writeError(w, http.StatusConflict, "that spell isn't on offer here right now")
			return
		}
		for _, learnedID := range state.LearnedSpells {
			if learnedID == itemID {
				writeError(w, http.StatusConflict, "you already know this spell")
				return
			}
		}
		if state.Gold < price {
			writeError(w, http.StatusConflict, fmt.Sprintf("not enough gold (need %d, have %d)", price, state.Gold))
			return
		}
		spell, ok := game.GetScrollSpell(itemID)
		if !ok {
			log.Printf("handlers: game: tavern_buy: ScrollPrices references unknown scroll %q", itemID)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		state.Gold -= price
		state.LearnedSpells = append(state.LearnedSpells, itemID)

		if err := h.saveState(userID, &state); err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		h.respondWithState(w, state, deathMarks, 0, []string{fmt.Sprintf("You learn %s for %d gold.", spell.Name, price)})
		return
	}

	if itemID == game.ClearDeathMarksItemID {
		if state.Gold < game.ClearDeathMarksPrice {
			writeError(w, http.StatusConflict, fmt.Sprintf("not enough gold (need %d, have %d)", game.ClearDeathMarksPrice, state.Gold))
			return
		}
		state.Gold -= game.ClearDeathMarksPrice
		if err := h.saveState(userID, &state); err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		if err := h.DB.ClearDeathMarks(userID); err != nil {
			log.Printf("handlers: game: tavern_buy: clear death marks for %s: %v", userID, err)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		h.respondWithState(w, state, 0, 0, []string{fmt.Sprintf("The tavern-keeper wipes your death marks clean for %d gold.", game.ClearDeathMarksPrice)})
		return
	}
	writeError(w, http.StatusBadRequest, "unknown tavern item_id")
}

// handleTavernRiddle both hands out and checks the tavern's riddle:
// `tavern_riddle` with no answer asks for (or re-shows) the current
// riddle; `tavern_riddle answer=<guess>` (frontend riddle.js sends
// this as the Answer field) checks a guess against it. See
// SaveState.CurrentRiddleID's doc comment on why the riddle has to be
// persisted between these two calls at all.
func (h *GameHandler) handleTavernRiddle(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int, answer string) {
	if !state.AtTavern {
		writeError(w, http.StatusConflict, "you need to be in the tavern to do that")
		return
	}

	if state.TavernRiddleSolved {
		h.respondWithState(w, state, deathMarks, 0, []string{"You've already earned the tavern's riddle reward this run — no more mana on offer."})
		return
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))

	// No answer submitted: hand out a riddle, or re-show the one
	// already in progress rather than rerolling it.
	if strings.TrimSpace(answer) == "" {
		if state.CurrentRiddleID == "" {
			riddle := game.RandomRiddle(rng)
			state.CurrentRiddleID = riddle.ID
			if err := h.saveState(userID, &state); err != nil {
				writeError(w, http.StatusInternalServerError, "internal server error")
				return
			}
			h.respondWithState(w, state, deathMarks, 0, []string{
				riddle.Question,
				`Answer with: tavern riddle <your answer>`,
			})
			return
		}
		riddle, ok := game.GetRiddle(state.CurrentRiddleID)
		if !ok {
			log.Printf("handlers: game: tavern_riddle: save state for %s has unknown riddle %q", userID, state.CurrentRiddleID)
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		h.respondWithState(w, state, deathMarks, 0, []string{
			riddle.Question,
			`Answer with: tavern riddle <your answer>`,
		})
		return
	}

	// An answer was submitted, but nothing was ever asked — tell the
	// player to ask for the riddle first rather than silently picking
	// one now (that'd let them skip straight to guessing blind).
	if state.CurrentRiddleID == "" {
		writeError(w, http.StatusConflict, "ask for the riddle first: tavern_riddle")
		return
	}

	riddle, ok := game.GetRiddle(state.CurrentRiddleID)
	if !ok {
		log.Printf("handlers: game: tavern_riddle: save state for %s has unknown riddle %q", userID, state.CurrentRiddleID)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	if strings.EqualFold(strings.TrimSpace(answer), riddle.Answer) {
		state.Mana += 3
		state.TavernRiddleSolved = true
		state.CurrentRiddleID = ""

		if err := h.saveState(userID, &state); err != nil {
			writeError(w, http.StatusInternalServerError, "internal server error")
			return
		}
		h.respondWithState(w, state, deathMarks, 0, []string{"Correct! You feel arcane energy well up inside you. (+3 mana)"})
		return
	}

	// Wrong guess: CurrentRiddleID deliberately stays set so the
	// player can keep trying the SAME riddle rather than getting a
	// fresh one (and a fresh chance to just guess-and-check) on every
	// wrong answer.
	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.respondWithState(w, state, deathMarks, 0, []string{"That's not it. Try again."})
}

// blackjackHandLines appends this round's hand(s) to lines: the
// player's full hand and total always, the dealer's either in full
// (revealDealer true — the round has ended) or just the up card with
// the hole card hidden (revealDealer false — the round is still being
// played). Shared by every response along a blackjack round so the
// "what does the table look like right now" text is worded identically
// whether it's shown mid-hand (start/hit) or at the very end
// (finishBlackjackRound).
func blackjackHandLines(state game.SaveState, revealDealer bool, lines []string) []string {
	playerTotal := game.BlackjackHandValue(state.BlackjackPlayerCards)
	lines = append(lines, fmt.Sprintf("Your hand: %s (%d)", strings.Join(state.BlackjackPlayerCards, ", "), playerTotal))
	if revealDealer {
		dealerTotal := game.BlackjackHandValue(state.BlackjackDealerCards)
		lines = append(lines, fmt.Sprintf("Dealer's hand: %s (%d)", strings.Join(state.BlackjackDealerCards, ", "), dealerTotal))
	} else if len(state.BlackjackDealerCards) > 0 {
		lines = append(lines, fmt.Sprintf("Dealer shows: %s, ?", state.BlackjackDealerCards[0]))
	}
	return lines
}

// finishBlackjackRound is the single place every blackjack resolution
// path (a natural on the deal, a bust on hit, or a full showdown after
// standing) funnels through: it applies this round's gold payout,
// clears the in-progress hand fields, and increments
// BlackjackRoundsPlayed — so the gold math and the per-run round
// counter can never drift out of sync with each other by one path
// forgetting a step the others remember. outcome is one of "blackjack"
// (natural 21, pays 3:2), "win" (pays 1:1), "push" (wager returned,
// i.e. no change), or "lose"/"bust" (wager forfeited — kept as two
// outcome strings rather than one so the narration can say which
// happened, even though they apply identical gold math).
func finishBlackjackRound(state *game.SaveState, outcome string, lines []string) []string {
	wager := state.BlackjackWager

	switch outcome {
	case "blackjack":
		win := wager * game.BlackjackNaturalPayoutNumerator / game.BlackjackNaturalPayoutDenominator
		state.Gold += win
		lines = append(lines, fmt.Sprintf("Blackjack! You beat the dealer with a natural 21. (+%d gold)", win))
	case "win":
		state.Gold += wager
		lines = append(lines, fmt.Sprintf("You beat the dealer. (+%d gold)", wager))
	case "push":
		lines = append(lines, "Push — you and the dealer tie. Your wager is returned.")
	case "lose":
		state.Gold -= wager
		lines = append(lines, fmt.Sprintf("The dealer wins this round. (-%d gold)", wager))
	case "bust":
		state.Gold -= wager
		lines = append(lines, fmt.Sprintf("You bust! The dealer wins without needing to play. (-%d gold)", wager))
	}

	state.BlackjackActive = false
	state.BlackjackWager = 0
	state.BlackjackPlayerCards = nil
	state.BlackjackDealerCards = nil
	state.BlackjackRoundsPlayed++

	lines = append(lines, fmt.Sprintf("Blackjack rounds played this run: %d/%d", state.BlackjackRoundsPlayed, game.BlackjackMaxRounds))
	return lines
}

// handleTavernBlackjack: `tavern_blackjack`. subAction ("" to start a
// fresh round, "hit", or "stand" — see actionRequest.ItemID's doc
// comment on this reuse) drives which of the three sub-flows below
// runs; amount is only read when starting a fresh round. Exactly one
// round can be in progress at a time (state.BlackjackActive) — starting
// a new one while one's already active just re-shows the in-progress
// hand instead of erroring, since that's a harmless, obvious mistake
// (re-running the same "deal me in" command) rather than a real
// conflict; hitting/standing with no round active IS an error, since
// there's nothing for either to act on.
func (h *GameHandler) handleTavernBlackjack(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int, subAction string, amount int) {
	if !state.AtTavern {
		writeError(w, http.StatusConflict, "you need to be in the tavern to do that")
		return
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	var lines []string

	switch strings.ToLower(strings.TrimSpace(subAction)) {
	case "hit":
		if !state.BlackjackActive {
			writeError(w, http.StatusConflict, `no blackjack round in progress — start one with "tavern blackjack <amount>"`)
			return
		}
		state.BlackjackPlayerCards = append(state.BlackjackPlayerCards, game.DrawBlackjackCard(rng))
		if game.BlackjackHandValue(state.BlackjackPlayerCards) > 21 {
			lines = blackjackHandLines(state, true, lines)
			lines = finishBlackjackRound(&state, "bust", lines)
		} else {
			lines = blackjackHandLines(state, false, lines)
			lines = append(lines, `Type "tavern blackjack hit" or "tavern blackjack stand".`)
		}

	case "stand":
		if !state.BlackjackActive {
			writeError(w, http.StatusConflict, `no blackjack round in progress — start one with "tavern blackjack <amount>"`)
			return
		}
		for game.DealerShouldHit(state.BlackjackDealerCards) {
			state.BlackjackDealerCards = append(state.BlackjackDealerCards, game.DrawBlackjackCard(rng))
		}
		playerTotal := game.BlackjackHandValue(state.BlackjackPlayerCards)
		dealerTotal := game.BlackjackHandValue(state.BlackjackDealerCards)

		lines = blackjackHandLines(state, true, lines)
		switch {
		case dealerTotal > 21:
			lines = append(lines, "The dealer busts!")
			lines = finishBlackjackRound(&state, "win", lines)
		case playerTotal > dealerTotal:
			lines = finishBlackjackRound(&state, "win", lines)
		case playerTotal < dealerTotal:
			lines = finishBlackjackRound(&state, "lose", lines)
		default:
			lines = finishBlackjackRound(&state, "push", lines)
		}

	case "", "start":
		if state.BlackjackActive {
			lines = blackjackHandLines(state, false, lines)
			lines = append(lines, `Finish this round first — type "tavern blackjack hit" or "tavern blackjack stand".`)
			h.respondWithState(w, state, deathMarks, 0, lines)
			return
		}
		if state.BlackjackRoundsPlayed >= game.BlackjackMaxRounds {
			writeError(w, http.StatusConflict, fmt.Sprintf("you've played all %d blackjack rounds this run", game.BlackjackMaxRounds))
			return
		}
		if amount < game.BlackjackMinWager || amount > game.BlackjackMaxWager {
			writeError(w, http.StatusBadRequest, fmt.Sprintf("you can wager between %d and %d gold", game.BlackjackMinWager, game.BlackjackMaxWager))
			return
		}
		if state.Gold < amount {
			writeError(w, http.StatusConflict, fmt.Sprintf("not enough gold (need %d, have %d)", amount, state.Gold))
			return
		}

		state.BlackjackActive = true
		state.BlackjackWager = amount
		state.BlackjackPlayerCards = []string{game.DrawBlackjackCard(rng), game.DrawBlackjackCard(rng)}
		state.BlackjackDealerCards = []string{game.DrawBlackjackCard(rng), game.DrawBlackjackCard(rng)}

		playerNatural := game.IsBlackjackNatural(state.BlackjackPlayerCards)
		dealerNatural := game.IsBlackjackNatural(state.BlackjackDealerCards)

		switch {
		case playerNatural && dealerNatural:
			lines = blackjackHandLines(state, true, lines)
			lines = finishBlackjackRound(&state, "push", lines)
		case playerNatural:
			lines = blackjackHandLines(state, true, lines)
			lines = finishBlackjackRound(&state, "blackjack", lines)
		case dealerNatural:
			lines = blackjackHandLines(state, true, lines)
			lines = finishBlackjackRound(&state, "lose", lines)
		default:
			lines = blackjackHandLines(state, false, lines)
			lines = append(lines, `Type "tavern blackjack hit" or "tavern blackjack stand".`)
		}

	default:
		writeError(w, http.StatusBadRequest, `unknown blackjack action — use "tavern blackjack <amount>", "tavern blackjack hit", or "tavern blackjack stand"`)
		return
	}

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.respondWithState(w, state, deathMarks, 0, lines)
}

// handleTavernRoulette: `tavern_roulette`. betSpec is "red", "black",
// "odd", "even", or a straight-up pocket number ("0"-"36" — see
// actionRequest.ItemID's doc comment); amount is the wager. Unlike
// blackjack, a spin fully resolves within this one call — there's no
// hand to persist across requests, so (unlike handleTavernBlackjack)
// this needs no SaveState fields of its own beyond the round counter.
func (h *GameHandler) handleTavernRoulette(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int, betSpec string, amount int) {
	if !state.AtTavern {
		writeError(w, http.StatusConflict, "you need to be in the tavern to do that")
		return
	}
	if state.RouletteRoundsPlayed >= game.RouletteMaxRounds {
		writeError(w, http.StatusConflict, fmt.Sprintf("you've played all %d roulette rounds this run", game.RouletteMaxRounds))
		return
	}
	if amount < game.RouletteMinWager || amount > game.RouletteMaxWager {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("you can wager between %d and %d gold", game.RouletteMinWager, game.RouletteMaxWager))
		return
	}
	if state.Gold < amount {
		writeError(w, http.StatusConflict, fmt.Sprintf("not enough gold (need %d, have %d)", amount, state.Gold))
		return
	}

	betSpec = strings.ToLower(strings.TrimSpace(betSpec))
	isStraight := false
	straightNumber := 0
	switch betSpec {
	case "red", "black", "odd", "even":
		// Even-money bet — nothing further to parse.
	case "":
		writeError(w, http.StatusBadRequest, `bet is required: "red", "black", "odd", "even", or a number 0-36`)
		return
	default:
		n, err := strconv.Atoi(betSpec)
		if err != nil || n < 0 || n > 36 {
			writeError(w, http.StatusBadRequest, `unknown bet — use "red", "black", "odd", "even", or a number 0-36`)
			return
		}
		isStraight = true
		straightNumber = n
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	pocket := game.SpinRoulette(rng)

	color := "green"
	if game.RouletteIsRed(pocket) {
		color = "red"
	} else if game.RouletteIsBlack(pocket) {
		color = "black"
	}
	lines := []string{fmt.Sprintf("The wheel spins... it lands on %d (%s).", pocket, color)}

	won := false
	payout := 0
	switch {
	case isStraight:
		won = pocket == straightNumber
		payout = amount * game.RouletteStraightPayoutMultiplier
	case betSpec == "red":
		won = game.RouletteIsRed(pocket)
		payout = amount * game.RouletteEvenMoneyPayoutMultiplier
	case betSpec == "black":
		won = game.RouletteIsBlack(pocket)
		payout = amount * game.RouletteEvenMoneyPayoutMultiplier
	case betSpec == "odd":
		won = game.RouletteIsOdd(pocket)
		payout = amount * game.RouletteEvenMoneyPayoutMultiplier
	case betSpec == "even":
		won = game.RouletteIsEven(pocket)
		payout = amount * game.RouletteEvenMoneyPayoutMultiplier
	}

	if won {
		state.Gold += payout
		lines = append(lines, fmt.Sprintf("You win! (+%d gold)", payout))
	} else {
		state.Gold -= amount
		lines = append(lines, fmt.Sprintf("No luck this spin. (-%d gold)", amount))
	}

	state.RouletteRoundsPlayed++
	lines = append(lines, fmt.Sprintf("Roulette rounds played this run: %d/%d", state.RouletteRoundsPlayed, game.RouletteMaxRounds))

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}
	h.respondWithState(w, state, deathMarks, 0, lines)
}

// handleTavernLeave: `tavern_leave`. Refuses once the run is complete
// — see handleDescend's doc comment on that being the enforcement
// point for "the tavern is the only place a finished run can act
// from." Also refuses mid-blackjack-hand (state.BlackjackActive) —
// leaving with a hand in progress would strand it against
// handleTavernBlackjack's "exactly one round in progress" invariant,
// so the player has to hit/stand it out (or bust) first.
func (h *GameHandler) handleTavernLeave(w http.ResponseWriter, userID string, state game.SaveState, deathMarks int) {
	if !state.AtTavern {
		writeError(w, http.StatusConflict, "you're not in the tavern")
		return
	}
	if state.BlackjackActive {
		writeError(w, http.StatusConflict, `finish your blackjack round first — "tavern blackjack hit" or "tavern blackjack stand"`)
		return
	}
	if state.IsRunComplete() {
		writeError(w, http.StatusConflict, "your run is complete — the tavern is the only place left for you now")
		return
	}

	state.AtTavern = false

	if err := h.saveState(userID, &state); err != nil {
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	h.respondWithState(w, state, deathMarks, 0, []string{"You step back out of the tavern. Stage 3 awaits below."})
}


// hasFamiliar reports whether the character currently holds a bonded
// familiar — primary (state.Familiar) or secondary
// (state.SecondFamiliar) — of the given kind. Mirrors the two-slot
// check already done at lines 414-427 for the response payload.
func hasFamiliar(state game.SaveState, kind game.FamiliarKind) bool {
	if state.Familiar != "" {
		if f, ok := game.GetFamiliar(state.Familiar); ok && f.Kind == kind {
			return true
		}
	}
	if state.SecondFamiliar != "" {
		if f, ok := game.GetFamiliar(state.SecondFamiliar); ok && f.Kind == kind {
			return true
		}
	}
	return false
}