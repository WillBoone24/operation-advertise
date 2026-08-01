/**
 * GameState.js
 * -----------------------------------------------------------------------
 * A client-side mirror of the authenticated player's profile, as
 * returned by GET /api/me. This is deliberately a DUMB mirror, not a
 * source of truth: nothing in this frontend should ever make a
 * decision based on stale local state when the backend's answer is one
 * request away, and nothing here is ever written back to the backend
 * directly (there is no save() method) — nothing to enforce yet, since
 * Phase 1 has no game endpoints, but noted now so the principle is
 * established before there's anything to accidentally violate it with.
 *
 * Structured as a class you instantiate and hold onto (in main.js),
 * not a module-level singleton — same dependency-injection preference
 * used throughout the backend (TokenManager, AuthHandler) and the
 * portfolio's easter egg (state.js's functions take no hidden global
 * state). Makes this trivial to reason about and to reset (e.g. on
 * logout, just construct a new one).
 * -----------------------------------------------------------------------
 */

import {
  me,
  getGameState,
  createCharacter as apiCreateCharacter,
  performAction as apiPerformAction,
  getBoardNotes as apiGetBoardNotes,
  postBoardNote as apiPostBoardNote,
  ApiError,
} from '../api/client.js';

/**
 * Maps one backend spellResponse (see handlers/game.go's
 * newSpellResponse) into this frontend's camelCase shape. Shared by
 * both `spells` (a Mage's known spells — starting kit + learned
 * scrolls) and `tavernSpells` (the two scrolls currently for sale)
 * below, since the backend sends the exact same fields for both —
 * one mapping, so they can't drift out of sync with each other.
 * @param {object} s
 */
function mapSpell(s) {
  return {
    id: s.id,
    name: s.name,
    description: s.description,
    manaCost: s.mana_cost,
    kind: s.kind, // 'damage' | 'heal'
    autoHit: !!s.auto_hit,
    damageDieSides: s.damage_die_sides || 0,
    damageDieCount: s.damage_die_count || 0,
    healPercentOfMaxHP: s.heal_percent_of_max_hp || 0,
  };
}

/**
 * Maps one backend tavernScrollResponse — mapSpell's fields plus the
 * gold price the tavern is charging for it this visit.
 * @param {object} s
 */
function mapTavernSpell(s) {
  return { ...mapSpell(s), price: s.price };
}

export class GameState {
  constructor() {
    this.userId = null;
    this.username = null;
    this.easterEggFound = false;
    this.level = null;
    this.storyCompleted = false;

    // False until load() has completed at least once. Commands that
    // display player data should check this first (see whoami.js)
    // rather than assuming these fields are populated — this class
    // can exist and be held onto before its first successful load.
    this.isLoaded = false;

    // --- Character/game fields, mirroring gameStateResponse (see
    // handlers/game.go's buildStateResponse) ---
    this.hasCharacter = false;
    this.characterName = null;
    this.class = null;
    this.difficulty = null;
    this.currentHP = null;
    this.maxHP = null;
    this.stats = null; // { str, dex, con } | null
    this.currentStage = null;
    this.currentPart = null;
    this.equipped = null; // { weaponId, armorId } | null
    this.inventory = [];
    this.inCombat = null; // { enemyId, enemyName, enemyCurrentHP, enemyMaxHP, enemies? } | null
    // enemies (only present during the Stage 10/Part 3 ambush) is
    // [{ id, name, currentHP, maxHP }] for all three simultaneous
    // combatants — see statusLine.js for how it's rendered instead of
    // the single-enemy fields above.
    this.pendingAdvance = false;
    this.runComplete = false;
    this.encounter = null; // { stage, part, description, isStageFinale } | null

    // Mage-only fields (see handlers/game.go's buildStateResponse) —
    // mana stays 0 and spells stays empty for every other class,
    // since the backend only ever populates them for a Mage
    // character.
    this.mana = 0;
    this.spells = []; // [{ id, name, description, manaCost }]

    // tavernSpells holds the two scroll spells (of game/tavern.go's
    // 7-spell pool) currently on offer in the tavern, resolved with
    // full inspect detail plus price — see mapTavernSpell above. Only
    // ever non-empty while atTavern is true; a fresh visit rolls a
    // different two (see game.RollTavernSpells).
    this.tavernSpells = []; // [{ id, name, description, manaCost, kind, autoHit, damageDieSides, damageDieCount, healPercentOfMaxHP, price }]

    // Death-cycle fields (see handlers/game.go's RecordDeath). marks
    // resets to 0 every 5th death, at which point lockedUntil is set
    // (an ISO-8601 string) and the account is locked out for 24h —
    // deathLock.js watches lockedUntil after every attack to force a
    // logout the moment it appears, rather than waiting for the next
    // request to bounce off a 423.
    this.deathMarks = 0;
    this.lockedUntil = null; // ISO-8601 string | null

    // --- Tavern fields (see game/tavern.go, handlers/game.go's
    // buildStateResponse) ---
    this.gold = 0;
    this.atTavern = false;
    this.monsterLore = null; // string[] | null — only populated once learned

    // Blackjack/roulette fields (see blackjack.go/roulette.go,
    // handlers/game.go's handleTavernBlackjack/handleTavernRoulette).
    // roundsPlayed are always populated (0 if never played); the
    // blackjack* fields below only mean anything while
    // blackjackActive is true — a round in progress, dealt but not
    // yet resolved. blackjackDealerCards only ever holds the dealer's
    // single up card while a round is active — the backend never
    // sends the hole card until the round has ended (see
    // gameStateResponse.BlackjackDealerCards' doc comment).
    this.blackjackRoundsPlayed = 0;
    this.rouletteRoundsPlayed = 0;
    this.blackjackActive = false;
    this.blackjackWager = 0;
    this.blackjackPlayerCards = []; // string[] — ranks, e.g. ["A", "10"]
    this.blackjackDealerCards = []; // string[] — just the up card while active

    // familiar/secondFamiliar mirror the backend's Familiar/
    // SecondFamiliar response fields (see handlers/game.go's
    // buildStateResponse) — { id, name, description } | null. A
    // character can hold up to two: familiar is the ordinary
    // random-drop companion, secondFamiliar is the guaranteed one
    // earned at the Journey's Ancient Woods finale (see
    // familiars.go's GrantSecondFamiliar).
    this.familiar = null;
    this.secondFamiliar = null;

    // dungeonComplete mirrors SaveState.DungeonComplete — true once
    // the dungeon's Stage 5 finale (the final boss) is cleared. The
    // tavern reached at that point is the one `tavern exit` (not
    // `tavern leave`) is meant for — see tavern.js.
    this.dungeonComplete = false;

    // See _applyGameState below for how these get populated.
    this.atKingsChambers = false;
    this.legacyPath = '';
    this.legacyPathName = '';

    // ability mirrors the backend's Ability response field (see
    // handlers/game.go's buildStateResponse) — { name, description,
    // usable } | null. Always populated once a character exists
    // (every class has exactly one ability); usable is false for
    // Mage (use "cast" instead) and Rogue (Sneak Attack is passive)
    // regardless of whether the once-per-stage resource has been
    // spent, since neither of those two classes ever consumes it.
    this.ability = null;

    // False until loadGame() (or an action that implies fresh state,
    // via applyActionResult) has completed at least once. Separate
    // from isLoaded since a player can have a valid profile session
    // with no character created yet — these two loading states are
    // genuinely independent, not two names for the same thing.
    this.isGameLoaded = false;
  }

  /**
   * Fetches the current profile from the backend and overwrites this
   * instance's fields. Call this once right after login succeeds, and
   * again whenever the displayed data needs to be refreshed.
   *
   * Deliberately does not catch errors — GET /api/me failing (401
   * because the session expired, network failure, etc.) is exactly
   * the kind of thing the caller needs to see and react to (e.g.
   * bounce back to login.js), not something this class should quietly
   * swallow.
   *
   * @returns {Promise<GameState>} resolves to `this`, so callers can
   *   write `const state = await new GameState().load();` inline if
   *   they don't need the instance held separately.
   */
  async load() {
    const profile = await me();

    this.userId = profile.user_id;
    this.username = profile.username;
    this.easterEggFound = profile.easter_egg_found;
    this.level = profile.level;
    this.storyCompleted = profile.story_completed;
    this.isLoaded = true;

    return this;
  }

  /**
   * Fetches the current character's state from GET /api/game/state
   * and overwrites this instance's game fields.
   *
   * Unlike load() above, a 404 here is NOT treated as an error — "no
   * character yet" is a normal, expected outcome (see
   * internal/game/state.go's UnmarshalSaveState, which makes the same
   * call on the backend), so it's caught here and turned into
   * hasCharacter = false rather than propagating. Any other failure
   * (network, 401, 500) still propagates, same as load().
   *
   * @returns {Promise<GameState>} resolves to `this`.
   */
  async loadGame() {
    try {
      const state = await getGameState();
      this._applyGameState(state);
    } catch (err) {
      if (err instanceof ApiError && err.status === 404) {
        this.hasCharacter = false;
      } else {
        throw err;
      }
    }
    this.isGameLoaded = true;
    return this;
  }

  /**
   * Creates a new character and applies the resulting state. Thin
   * wrapper around api/client.js's createCharacter — see this class's
   * doc comment on why command files call THIS, not client.js
   * directly.
   * @param {string} characterName
   * @param {string} className
   * @param {string} difficulty
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async createCharacter(characterName, className, difficulty) {
    const result = await apiCreateCharacter(characterName, className, difficulty);
    return this.applyActionResult(result);
  }

  /**
   * Resolves one attack action. See handlers/game.go's handleAttack:
   * a single call may both spawn a new fight (if not already in one)
   * and resolve a full round (player swing, then enemy counterswing
   * if it survives) — this method doesn't need to know which case
   * it's in, it just applies whatever state comes back.
   *
   * targetId only matters during a simultaneous multi-enemy fight
   * (gameState.inCombat.enemies non-null — currently just the Stage
   * 10/Part 3 ambush): one of the living enemies' IDs, e.g.
   * "thaddeus". Ignored (and unnecessary) for every ordinary
   * single-enemy fight, which has nothing to target between.
   * @param {string} [targetId]
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async attack(targetId) {
    const result = await apiPerformAction('attack', undefined, undefined, undefined, targetId);
    return this.applyActionResult(result);
  }

  /**
   * Equips an armor piece from inventory. See items.go's design note
   * on weapons being permanently non-swappable — passing a weapon ID
   * here is rejected by the backend, not pre-validated on this side.
   * @param {string} itemId
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async equip(itemId) {
    const result = await apiPerformAction('equip', itemId);
    return this.applyActionResult(result);
  }

  /**
   * Consumes a potion from inventory. See items.go's PotionKind doc
   * comment — stat potions are a permanent, per-use random roll, HP
   * potions are a one-time full heal.
   * @param {string} itemId
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async usePotion(itemId) {
    const result = await apiPerformAction('use', itemId);
    return this.applyActionResult(result);
  }

  /**
   * Casts one of the Mage's permanent spells. Mage-only — the backend
   * rejects this for every other class (see handlers/game.go's
   * handleCast). Like attack(), a single call may both spawn a fresh
   * fight and resolve a full round, including the enemy's counterswing
   * if it survives.
   *
   * targetId mirrors attack()'s — only meaningful for a damage spell
   * cast during a simultaneous multi-enemy fight; ignored otherwise.
   * @param {string} spellId
   * @param {string} [targetId]
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async castSpell(spellId, targetId) {
    const result = await apiPerformAction('cast', undefined, spellId, undefined, targetId);
    return this.applyActionResult(result);
  }

  /**
   * Uses the character's class ability: Fighter's Second Wind,
   * Cleric's Mend, or Ranger's Steady Aim. Rejected by the backend
   * for Mage (spells go through castSpell instead) and Rogue (Sneak
   * Attack is passive — see handlers/game.go's handleAbility) — this
   * method doesn't pre-check gameState.ability.usable itself, same
   * "surface whatever the backend says" convention every other
   * action method here follows. Like attack()/castSpell(), a single
   * call may both spawn a fresh fight and resolve a full round,
   * including the enemy's counterswing if it survives.
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async useAbility() {
    const result = await apiPerformAction('ability');
    return this.applyActionResult(result);
  }

  /**
   * Advances past a cleared encounter. Only valid when pendingAdvance
   * is true — see state.go's PendingAdvance doc comment. The backend
   * enforces this on its own; this method doesn't pre-check, it just
   * surfaces whatever error the backend returns if called too early.
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async descend() {
    const result = await apiPerformAction('descend');
    return this.applyActionResult(result);
  }

  // --- Tavern actions (see game/tavern.go, handlers/game.go's
  // handleTavern*) — all four require atTavern to already be true;
  // the backend enforces this itself (409 otherwise), these methods
  // don't pre-check it any more than descend() pre-checks
  // pendingAdvance, per this class's existing "surface whatever the
  // backend says" convention. ---

  /**
   * Learns (or re-confirms) the spell-effectiveness lore. Idempotent
   * on the backend — safe to call more than once.
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async tavernLore() {
    const result = await apiPerformAction('tavern_lore');
    return this.applyActionResult(result);
  }

  /**
   * Buys a tonic (from game.TavernPotions) or a spell scroll (from
   * game.ScrollPrices) — both ID spaces are checked server-side, so
   * this method doesn't need to know which kind itemId refers to.
   * @param {string} itemId
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async tavernBuy(itemId) {
    const result = await apiPerformAction('tavern_buy', itemId);
    return this.applyActionResult(result);
  }

  /**
   * Asks for (or re-shows) the tavern's riddle when called with no
   * answer, or checks a guess against it when one is given. See
   * handlers/game.go's handleTavernRiddle for the full two-call
   * shape this mirrors.
   * @param {string} [answer]
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async tavernRiddle(answer) {
    const result = await apiPerformAction('tavern_riddle', undefined, undefined, answer);
    return this.applyActionResult(result);
  }

  /**
   * Starts a fresh blackjack round, wagering `amount` gold. Rejected
   * by the backend if a round is already in progress, the per-run
   * round cap is reached, the wager is out of range, or there isn't
   * enough gold — this method doesn't pre-check any of that, same
   * "surface whatever the backend says" convention as tavernBuy.
   * @param {number} amount
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async tavernBlackjackStart(amount) {
    const result = await apiPerformAction('tavern_blackjack', '', undefined, undefined, undefined, amount);
    return this.applyActionResult(result);
  }

  /**
   * Takes another card in the current blackjack round. Rejected by
   * the backend if no round is in progress.
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async tavernBlackjackHit() {
    const result = await apiPerformAction('tavern_blackjack', 'hit');
    return this.applyActionResult(result);
  }

  /**
   * Stands on the current blackjack hand, letting the dealer play out
   * and the round resolve. Rejected by the backend if no round is in
   * progress.
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async tavernBlackjackStand() {
    const result = await apiPerformAction('tavern_blackjack', 'stand');
    return this.applyActionResult(result);
  }

  /**
   * Spins the roulette wheel once, wagering `amount` gold on `bet` —
   * "red", "black", "odd", "even", or a straight-up number 0-36 (as a
   * string). Resolves immediately; unlike blackjack there's no
   * multi-step hand to track between calls.
   * @param {string} bet
   * @param {number} amount
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async tavernRoulette(bet, amount) {
    const result = await apiPerformAction('tavern_roulette', bet, undefined, undefined, undefined, amount);
    return this.applyActionResult(result);
  }

  /**
   * Leaves the tavern, resuming the dungeon at Stage 3. Rejected by
   * the backend once the run is complete — see SaveState.AtTavern's
   * doc comment on the tavern being the only place a finished run can
   * act from.
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async tavernLeave() {
    const result = await apiPerformAction('tavern_leave');
    return this.applyActionResult(result);
  }

  // --- Tavern community board (see handlers/board.go) ---
  // Deliberately NOT routed through applyActionResult — reading/
  // posting the board doesn't return or mutate a character's
  // SaveState at all (it's a shared resource, not per-character), so
  // there's no gameStateResponse here to apply.

  /**
   * Fetches the community board's notes: `pinned` (paid, permanent —
   * ALWAYS the full set, never truncated) and `notes` (free, subject
   * to board.js's cycling display and the backend's 50-note cap).
   * Rejected by the backend (409) if not currently atTavern.
   * @returns {Promise<{
   *   pinned: Array<{username: string, message: string, createdAt: string}>,
   *   notes: Array<{username: string, message: string, createdAt: string}>
   * }>}
   */
  async getBoardNotes() {
    const result = await apiGetBoardNotes();
    const map = (n) => ({ username: n.username, message: n.message, createdAt: n.created_at });
    return {
      pinned: (result.pinned || []).map(map),
      notes: (result.notes || []).map(map),
    };
  }

  /**
   * Posts a note to the community board. Same atTavern gate as
   * getBoardNotes. pinned requests the paid, permanent variant —
   * costs gold server-side (see handlers/board.go's PostNote); the
   * caller should check gameState.gold itself first for a friendly
   * error rather than relying on the request failing.
   * @param {string} message
   * @param {boolean} [pinned]
   * @returns {Promise<void>}
   */
  async postBoardNote(message, pinned) {
    await apiPostBoardNote(message, pinned);
    if (pinned) {
      // Gold was deducted server-side — refresh so gold/atTavern etc.
      // reflect it immediately rather than waiting for the next
      // unrelated action to happen to reload state.
      await this.loadGame();
    }
  }

  /**
   * Applies an actionResultResponse — the shape returned by
   * POST /api/game/create and POST /api/game/action, i.e.
   * { state, log }. Public (unlike _applyGameState) because commands
   * that get a result back directly from a GameState action method
   * never need this themselves — it's called internally by
   * createCharacter()/attack()/equip()/descend() above — but it's
   * kept accessible in case a future command needs to apply a result
   * it obtained some other way.
   * @param {{state: Object, log: string[]}} actionResult
   * @returns {string[]} the log lines from this action
   */
  applyActionResult(actionResult) {
    this._applyGameState(actionResult.state);
    return actionResult.log;
  }

  /**
   * Shared field-mapping helper for loadGame() and applyActionResult()
   * — both receive the same gameStateResponse shape from the backend,
   * just via different endpoints/wrappers. Centralizing the mapping
   * here means the two callers can't drift out of sync with each
   * other the way two independent copies of this mapping eventually
   * would.
   * @private
   */
  _applyGameState(state) {
    this.hasCharacter = true;
    this.characterName = state.character_name;
    this.class = state.class;
    this.difficulty = state.difficulty;
    this.currentHP = state.current_hp;
    this.maxHP = state.max_hp;
    this.stats = state.stats ? { str: state.stats.str, dex: state.stats.dex, con: state.stats.con } : null;
    this.currentStage = state.current_stage;
    this.currentPart = state.current_part;
    this.equipped = { weaponId: state.equipped.weapon_id, armorId: state.equipped.armor_id };
    this.inventory = state.inventory;
    this.inCombat = state.in_combat
      ? {
          enemyId: state.in_combat.enemy_id,
          enemyName: state.in_combat.enemy_name,
          enemyCurrentHP: state.in_combat.enemy_current_hp,
          enemyMaxHP: state.in_combat.enemy_max_hp,
          // enemies is only ever present for a simultaneous multi-enemy
          // fight (currently just the Stage 10/Part 3 ambush) — every
          // ordinary fight leaves it undefined, same "omit rather than
          // populate dead data" convention the rest of this mapping
          // follows. See statusLine.js for how the two shapes render
          // differently.
          enemies: state.in_combat.enemies
            ? state.in_combat.enemies.map((e) => ({
                id: e.id,
                name: e.name,
                currentHP: e.current_hp,
                maxHP: e.max_hp,
              }))
            : undefined,
        }
      : null;
    this.pendingAdvance = state.pending_advance;
    this.runComplete = state.run_complete;
    this.deathMarks = state.death_marks;
    // Only overwrite lockedUntil when the backend actually sent one —
    // it's omitted (undefined) on every ordinary response, and this
    // field should only ever hold the single fresh lockout timestamp
    // set by the attack that triggered it (see gameStateResponse's
    // doc comment in handlers/game.go), not get clobbered back to
    // null by ​the next unrelated action's response.
    if (state.locked_until) {
      this.lockedUntil = state.locked_until;
    }
    this.encounter = state.encounter
      ? {
          stage: state.encounter.stage,
          part: state.encounter.part,
          description: state.encounter.description,
          isStageFinale: state.encounter.is_stage_finale,
        }
      : null;

    // Only ever present in the response for a Mage character — reset
    // to the "no spells" defaults otherwise, rather than leaking a
    // stale mana/spell list from before a hypothetical class change.
    this.mana = state.mana || 0;
    this.spells = state.spells ? state.spells.map(mapSpell) : [];

    this.gold = state.gold || 0;
    this.atTavern = !!state.at_tavern;
    // monsterLore is only ever sent once learned (see
    // gameStateResponse's doc comment) — once learned it stays known
    // for the rest of the run, so a response that omits it (a later,
    // unrelated action) should NOT wipe out lore already learned and
    // displayed. Only overwrite when the backend actually sent it.
    if (state.monster_lore) {
      this.monsterLore = state.monster_lore;
    }

    // tavernSpells mirrors gameStateResponse.TavernSpells — the two
    // scroll spells (of game/tavern.go's 7-spell pool) currently on
    // offer, resolved with full inspect detail plus price. Unlike
    // monsterLore above, this one IS overwritten unconditionally
    // (including back to []) on every response that omits it: once
    // atTavern goes false the offer is gone, not "still true but not
    // mentioned this time," so stale scrolls from a prior visit should
    // never linger in the shop listing or "inspect" output.
    this.tavernSpells = state.tavern_spells ? state.tavern_spells.map(mapTavernSpell) : [];

    // Blackjack/roulette fields — roundsPlayed are always sent (0 or
    // more), overwritten unconditionally like tavernSpells above (a
    // real count, never "still true but omitted this time"). The
    // in-hand fields are only ever sent while blackjackActive is true
    // (see gameStateResponse's doc comment), so they're reset to their
    // empty defaults whenever the backend omits them — a round that
    // just ended (win/lose/bust) should immediately stop showing a
    // hand, the same way tavernSpells clears once atTavern goes false.
    this.blackjackRoundsPlayed = state.blackjack_rounds_played || 0;
    this.rouletteRoundsPlayed = state.roulette_rounds_played || 0;
    this.blackjackActive = !!state.blackjack_active;
    this.blackjackWager = state.blackjack_active ? state.blackjack_wager || 0 : 0;
    this.blackjackPlayerCards = state.blackjack_active ? state.blackjack_player_cards || [] : [];
    this.blackjackDealerCards = state.blackjack_active ? state.blackjack_dealer_cards || [] : [];

    this.familiar = state.familiar
      ? { id: state.familiar.id, name: state.familiar.name, description: state.familiar.description }
      : null;
    this.secondFamiliar = state.second_familiar
      ? { id: state.second_familiar.id, name: state.second_familiar.name, description: state.second_familiar.description }
      : null;
    this.dungeonComplete = !!state.dungeon_complete;

    // atKingsChambers/legacyPath/legacyPathName mirror the backend's
    // AtKingsChambers/LegacyPath/LegacyPathName response fields (see
    // handlers/game.go's buildStateResponse). legacyPath/Name stay
    // '' until handleChoosePath actually sets them — this class never
    // invents a display name locally, same "server resolves display
    // fields" convention familiar/secondFamiliar already follow.
    this.atKingsChambers = !!state.at_kings_chambers;
    this.legacyPath = state.legacy_path || '';
    this.legacyPathName = state.legacy_path_name || '';

    this.ability = state.ability
      ? { name: state.ability.name, description: state.ability.description, usable: !!state.ability.usable }
      : null;
  }

  /**
   * Leaves the post-dungeon tavern waypoint and begins the Journey
   * (Stage 6+). Rejected by the backend (409) unless dungeonComplete
   * is true — see handlers/game.go's handleExitDungeon.
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async exitDungeon() {
    const result = await apiPerformAction('exit_dungeon');
    return this.applyActionResult(result);
  }

  // --- King's chambers (see game/legacy.go, handlers/game.go's
  // handleLegacyHall/handleChoosePath) — the true end of a run, only
  // reachable once atKingsChambers is true. Both require that server-
  // side, same "surface whatever the backend says" convention as the
  // tavern methods above. ---

  /**
   * Shows the king's chambers: the Hall of Legacies (everyone who's
   * finished the game before this character) and, if this character
   * hasn't chosen a path yet, the three paths on offer. Safe to call
   * more than once — idempotent on the backend.
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async legacyHall() {
    const result = await apiPerformAction('legacy_hall');
    return this.applyActionResult(result);
  }

  /**
   * Permanently commits this character to one of the three legacy
   * paths ("lords" | "commons" | "heroes"). Rejected by the backend
   * (409) if a path was already chosen — see state.go's LegacyPath
   * doc comment. This is the last action a character can ever take.
   * @param {string} pathId
   * @returns {Promise<string[]>} narration lines from the backend
   */
  async choosePath(pathId) {
    const result = await apiPerformAction('choose_path', pathId);
    return this.applyActionResult(result);
  }
}