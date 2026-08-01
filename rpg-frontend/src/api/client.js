/**
 * client.js
 * -----------------------------------------------------------------------
 * The RPG frontend's sole point of contact with the backend. Mirrors the
 * fetch-wrapping pattern already established in the portfolio's
 * registration.js (network-error vs. non-OK-response handling kept
 * separate, backend's own {"error": "..."} messages surfaced directly)
 * but adds token persistence, since this frontend needs to stay logged
 * in across page loads the way the portfolio's one-shot registration
 * flow never had to.
 *
 * No other file in this project should call fetch() directly against
 * the backend — everything routes through here, same principle as
 * "database/users.go is the only file allowed to write SQL" on the
 * backend side.
 * -----------------------------------------------------------------------
 */

// Read from the generated config rather than hardcoded — see
// ../config.js's doc comment for why (WSL2 localhost forwarding is
// broken in this dev environment, so the backend is addressed by its
// current WSL IP instead, refreshed once per session by
// scripts/dev-up.sh).
import { API_BASE_URL } from '../config.js';

// Namespaced distinctly from the portfolio site's localStorage keys
// (ee_auth_token / ee_user_id). These are two separate origins with
// separate localStorage anyway, so collision isn't actually possible —
// but distinct names make it unambiguous which app a key belongs to if
// you're ever inspecting storage while both sites are open in the same
// browser's dev tools history.
const STORAGE_KEYS = {
  TOKEN: 'rpg_auth_token',
  USER_ID: 'rpg_user_id',
};

/**
 * Thrown for any non-2xx response from the backend. Callers (login.js,
 * future game commands) catch this and inspect `.status` /
 * `.message` to decide how to react — e.g. a 401 during login means
 * "wrong credentials," while a 401 during an authenticated game
 * request means "session expired, back to login."
 */
export class ApiError extends Error {
  constructor(status, message) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

/**
 * Thrown when fetch() itself fails (network down, DNS failure, CORS
 * rejection) — distinct from ApiError, which represents the backend
 * responding but with an error status. Callers generally want to
 * handle these two cases with different messaging ("can't reach the
 * server" vs. "server said no"), same distinction registration.js
 * draws on the portfolio side.
 */
export class NetworkError extends Error {
  constructor(cause) {
    super('Could not reach the server.');
    this.name = 'NetworkError';
    this.cause = cause;
  }
}

// ---------------------------------------------------------------------
// Token persistence
// ---------------------------------------------------------------------

export function getToken() {
  return localStorage.getItem(STORAGE_KEYS.TOKEN);
}

export function getUserId() {
  return localStorage.getItem(STORAGE_KEYS.USER_ID);
}

export function isAuthenticated() {
  return getToken() !== null;
}

function setSession(token, userId) {
  localStorage.setItem(STORAGE_KEYS.TOKEN, token);
  localStorage.setItem(STORAGE_KEYS.USER_ID, userId);
}

/**
 * Clears the stored session. Exported (not just used internally) since
 * login.js needs to call this on an explicit "logout," and the future
 * game loop needs to call it when a request comes back 401 (session
 * expired — see request() below).
 */
export function clearSession() {
  localStorage.removeItem(STORAGE_KEYS.TOKEN);
  localStorage.removeItem(STORAGE_KEYS.USER_ID);
}

// ---------------------------------------------------------------------
// Core request wrapper
// ---------------------------------------------------------------------

/**
 * Performs a request against the backend, attaching the stored auth
 * token if one exists and `skipAuth` isn't set. Parses the JSON body
 * (success or error) and throws ApiError for non-2xx responses.
 *
 * @param {string} path - e.g. '/api/login', '/api/me'
 * @param {Object} [opts]
 * @param {string} [opts.method='GET']
 * @param {Object} [opts.body] - will be JSON.stringify'd if provided
 * @param {boolean} [opts.skipAuth=false] - omit the Authorization
 *   header even if a token is stored (not currently used by any
 *   endpoint this frontend calls, since /api/login doesn't need it —
 *   included for completeness/future endpoints rather than left
 *   unhandled).
 * @returns {Promise<any>} the parsed JSON response body
 * @throws {ApiError} on a non-2xx response
 * @throws {NetworkError} if the request itself fails
 */
async function request(path, { method = 'GET', body, skipAuth = false } = {}) {
  const headers = { 'Content-Type': 'application/json' };

  const token = getToken();
  if (token && !skipAuth) {
    headers['Authorization'] = `Bearer ${token}`;
  }

  let response;
  try {
    response = await fetch(`${API_BASE_URL}${path}`, {
      method,
      headers,
      body: body !== undefined ? JSON.stringify(body) : undefined,
    });
  } catch (cause) {
    throw new NetworkError(cause);
  }

  let parsedBody;
  try {
    parsedBody = await response.json();
  } catch {
    parsedBody = null;
  }

  if (!response.ok) {
    // A 401 on an AUTHENTICATED request means the session has
    // expired or been invalidated server-side — proactively clearing
    // it here means every future request skips straight to "no
    // token" behavior instead of repeatedly sending a dead token.
    // (A 401 from /api/login itself — wrong credentials — also
    // clears via this same path, which is harmless: there was never
    // a valid session to lose in that case.)
    if (response.status === 401) {
      clearSession();
    }

    const message = parsedBody?.error || `Request failed (${response.status}).`;
    throw new ApiError(response.status, message);
  }

  return parsedBody;
}

// ---------------------------------------------------------------------
// Endpoint-specific convenience methods
// ---------------------------------------------------------------------
// Deliberately narrow: only the two endpoints this login-only frontend
// actually needs. No register() here — per the earlier decision,
// registration stays exclusive to the portfolio's easter egg. Adding
// more endpoints (game actions) is future work once the backend
// actually has a game engine to call.

/**
 * Logs in with a username/password, storing the returned session on
 * success.
 * @param {string} username
 * @param {string} password
 * @returns {Promise<{ token: string, user_id: string }>}
 */
export async function login(username, password) {
  const result = await request('/api/login', {
    method: 'POST',
    body: { username, password },
    skipAuth: true, // logging in obviously can't attach a token yet
  });

  setSession(result.token, result.user_id);
  return result;
}

/**
 * Fetches the authenticated player's profile. Requires a stored token
 * (set by a prior login()) — if none exists, the backend will reject
 * with 401 and this throws ApiError as usual.
 * @returns {Promise<{ user_id: string, username: string, easter_egg_found: boolean, level: number, story_completed: boolean }>}
 */
export function me() {
  return request('/api/me');
}

// ---------------------------------------------------------------------
// Game endpoints
// ---------------------------------------------------------------------
// Deliberately thin wrappers, same as login()/me() above — request
// parsing/error handling lives entirely in request(); these just name
// the endpoint and shape the body. GameState.js is the only caller of
// these three; individual command files never import client.js
// directly (see help.js's ctx.commands doc comment on keeping that
// boundary clean).

/**
 * Creates a new character. The backend rejects this with 409 if a
 * character already exists for the account (see
 * handlers/game.go's CreateCharacter) — callers should catch ApiError
 * and check .status rather than assuming this always succeeds.
 * @param {string} characterName
 * @param {string} className - one of game.ClassID's values (e.g. "fighter")
 * @param {string} difficulty - "easy" | "hard"
 * @returns {Promise<{state: Object, log: string[]}>} an actionResultResponse
 */
export function createCharacter(characterName, className, difficulty) {
  return request('/api/game/create', {
    method: 'POST',
    body: { character_name: characterName, class: className, difficulty },
  });
}

/**
 * Fetches the current character's full state. Throws ApiError with
 * status 404 if no character has been created yet — GameState.loadGame()
 * is the sanctioned place that gets handled, not this function.
 * @returns {Promise<Object>} a gameStateResponse (see handlers/game.go)
 */
export function getGameState() {
  return request('/api/game/state');
}

/**
 * Performs one game action: attack, equip, descend, cast, use, or one
 * of the tavern_* actions. itemId is used by "equip"/"use" (an item
 * ID), "tavern_buy" (a tonic or scroll ID), "tavern_blackjack" ("" to
 * start a round, "hit", or "stand"), and "tavern_roulette" (the bet:
 * "red"/"black"/"odd"/"even" or a number 0-36); spellId only by
 * "cast"; answer only by "tavern_riddle"; targetId only by
 * "attack"/"cast" during the Stage 10/Part 3 simultaneous ambush (one
 * of "thaddeus"/"alfonse"/"aragorn" — see handlers/game.go's
 * actionRequest doc comment); amount is the gold wager for
 * "tavern_blackjack" (only read when starting a round) and every
 * "tavern_roulette" spin. The backend ignores whichever of these a
 * given action type doesn't need (see handlers/game.go's Action
 * dispatch).
 * @param {'attack'|'equip'|'use'|'cast'|'descend'|'tavern_lore'|'tavern_buy'|'tavern_riddle'|'tavern_blackjack'|'tavern_roulette'|'tavern_leave'} action
 * @param {string} [itemId]
 * @param {string} [spellId]
 * @param {string} [answer]
 * @param {string} [targetId]
 * @param {number} [amount]
 * @returns {Promise<{state: Object, log: string[]}>} an actionResultResponse
 */
export function performAction(action, itemId, spellId, answer, targetId, amount) {
  const body = { action };
  if (itemId) body.item_id = itemId;
  if (spellId) body.spell_id = spellId;
  if (answer) body.answer = answer;
  if (targetId) body.target_id = targetId;
  if (amount) body.amount = amount;
  return request('/api/game/action', { method: 'POST', body });
}

// ---------------------------------------------------------------------
// Tavern community board endpoints
// ---------------------------------------------------------------------
// Separate from performAction — posting/reading the board doesn't
// mutate a character's SaveState the way every game/action branch
// does, it's a shared resource across all players (see
// handlers/board.go), so it gets its own thin wrappers rather than
// being shoehorned into the action shape above.

/**
 * Fetches the tavern community board's recent notes, newest first.
 * The backend rejects this with a 409 if the character isn't
 * currently AtTavern (see handlers/board.go's ListNotes).
 * @returns {Promise<{ notes: Array<{username: string, message: string, created_at: string}> }>}
 */
export function getBoardNotes() {
  return request('/api/game/board');
}

/**
 * Posts a note to the tavern community board. Same AtTavern gate as
 * getBoardNotes (see handlers/board.go's PostNote). pinned requests
 * the paid, permanent variant (handlers/board.go's
 * pinnedNoteGoldCost gold, deducted server-side) instead of a normal
 * free note.
 * @param {string} message
 * @param {boolean} [pinned]
 * @returns {Promise<{status: string}>}
 */
export function postBoardNote(message, pinned) {
  return request('/api/game/board', { method: 'POST', body: { message, pinned: !!pinned } });
}