/**
 * state.js
 * -----------------------------------------------------------------------
 * Single source of truth for the easter egg's stage progression.
 *
 * No other module in this system reads/writes localStorage directly, and
 * no other module decides "am I allowed to activate yet" on its own —
 * they all ask THIS module. That's the structural guarantee that later
 * stages can't appear or activate early: it's not a UI convention, it's
 * enforced by every stage module calling canActivate(stage) before doing
 * anything visible or interactive.
 *
 * Stage order (linear, each requires the previous):
 *   1. LOGO_CLICKED   — the DVD-bouncing logo has been clicked
 *   2. PUZZLE_COMPLETE — all 8 letters of "THANK YOU" have been found,
 *      hidden as single characters inside index.html's existing copy
 *   3. REGISTERED      — the user has successfully registered with the backend
 *
 * "Discovery complete" (the one-time-experience flag) is a SEPARATE
 * concept from stage progression — see isDiscoveryComplete() below.
 * -----------------------------------------------------------------------
 */

// Storage keys are namespaced with "ee_" (easter egg) to avoid any
// collision with other localStorage usage this site might add later.
const STORAGE_KEYS = {
  DISCOVERY_COMPLETE: 'ee_discovery_complete',
  CURRENT_STAGE: 'ee_current_stage',
  COLLECTED_LETTERS: 'ee_collected_letters',
};

// The ordered list of stages. Array index = progression order, which
// canActivate() below uses to enforce "can't skip ahead."
export const STAGES = Object.freeze({
  IDLE: 'idle',
  LOGO_CLICKED: 'logo_clicked',
  PUZZLE_COMPLETE: 'puzzle_complete',
  REGISTERED: 'registered',
});

const STAGE_ORDER = [
  STAGES.IDLE,
  STAGES.LOGO_CLICKED,
  STAGES.PUZZLE_COMPLETE,
  STAGES.REGISTERED,
];

/**
 * Reads the current stage from localStorage. Defaults to IDLE if unset
 * or if the stored value isn't a recognized stage (defensive against a
 * corrupted/hand-edited localStorage value — falling back to IDLE is
 * always safe, since it's the most restrictive state).
 */
function getCurrentStage() {
  const raw = localStorage.getItem(STORAGE_KEYS.CURRENT_STAGE);
  return STAGE_ORDER.includes(raw) ? raw : STAGES.IDLE;
}

/**
 * Advances the stored stage to `stage`, but ONLY if it is a forward
 * move (or a no-op re-write of the current stage). This is the actual
 * enforcement mechanism: even if a caller had a bug and tried to set
 * PUZZLE_COMPLETE while still at IDLE (skipping LOGO_CLICKED), this
 * function silently refuses rather than corrupting progression state.
 *
 * Returns true if the stage was updated, false if the move was invalid.
 */
export function advanceStage(stage) {
  if (!STAGE_ORDER.includes(stage)) {
    console.error(`[easter-egg] advanceStage: unknown stage "${stage}"`);
    return false;
  }

  const currentIndex = STAGE_ORDER.indexOf(getCurrentStage());
  const targetIndex = STAGE_ORDER.indexOf(stage);

  // Only allow moving exactly one step forward, or re-confirming the
  // current stage. This is stricter than "any forward move" on purpose
  // — it means a stage module can never accidentally leapfrog an
  // intermediate stage due to a logic bug, since the only valid
  // transition is "the very next one."
  if (targetIndex !== currentIndex && targetIndex !== currentIndex + 1) {
    console.warn(
      `[easter-egg] advanceStage: rejected out-of-order transition ` +
      `(${getCurrentStage()} -> ${stage})`
    );
    return false;
  }

  localStorage.setItem(STORAGE_KEYS.CURRENT_STAGE, stage);
  return true;
}

/**
 * The core gate every stage module calls before activating itself.
 * "Can I turn on right now?" — true only if the PREVIOUS stage in the
 * order has already been reached (or this stage itself has, covering
 * page-refresh mid-sequence).
 *
 * Example: letter-puzzle.js calls canActivate(STAGES.LOGO_CLICKED)
 * before making its characters bold/clickable. If the visitor refreshes
 * the page after clicking the logo but before finishing the puzzle,
 * this correctly returns true and the puzzle re-activates in place.
 */
export function canActivate(requiredStage) {
  const currentIndex = STAGE_ORDER.indexOf(getCurrentStage());
  const requiredIndex = STAGE_ORDER.indexOf(requiredStage);
  return currentIndex >= requiredIndex;
}

/**
 * True once the ENTIRE sequence (through registration) has been
 * completed. Distinct from getCurrentStage() reaching REGISTERED,
 * because discovery completion is a permanent, deliberate flag set
 * once at the end — see markDiscoveryComplete().
 */
export function isDiscoveryComplete() {
  return localStorage.getItem(STORAGE_KEYS.DISCOVERY_COMPLETE) === 'true';
}

/**
 * Marks the discovery experience as permanently complete for this
 * browser. Called once, by registration.js, after a successful
 * registration response from the backend — never by any earlier stage.
 *
 * This is what main.js checks on every page load to decide whether to
 * mount the overlay at all. Once true, the moving logo, puzzle, and
 * registration form are never shown again in this browser (per the
 * spec's "one-time discovery" + "browser-based tracking" requirements).
 */
export function markDiscoveryComplete() {
  localStorage.setItem(STORAGE_KEYS.DISCOVERY_COMPLETE, 'true');
}

/**
 * Stage 2's letters are now woven directly into index.html's copy
 * rather than rendered inside an overlay, so they persist in the DOM
 * across a page reload while the visitor is still mid-hunt. That means
 * letter-puzzle.js needs its own small piece of session state — which
 * letters of "THANK YOU" have already been found — so a reload doesn't
 * un-find letters the visitor already clicked. Per this file's own
 * rule ("no other module reads/writes localStorage directly"), that
 * state lives here, not in letter-puzzle.js.
 */

/**
 * Returns the array of collected letters (e.g. ['T', 'O']) for the
 * in-progress hunt. Always an array, even if storage is empty or
 * corrupted — same defensive default as getCurrentStage().
 */
export function getCollectedLetters() {
  const raw = localStorage.getItem(STORAGE_KEYS.COLLECTED_LETTERS);
  if (!raw) return [];
  try {
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed : [];
  } catch {
    return [];
  }
}

/**
 * Records a single letter as found. Idempotent — collecting the same
 * letter twice is a no-op rather than a duplicate entry.
 */
export function addCollectedLetter(letter) {
  const current = new Set(getCollectedLetters());
  current.add(letter);
  localStorage.setItem(STORAGE_KEYS.COLLECTED_LETTERS, JSON.stringify(Array.from(current)));
}

/**
 * Clears letter-hunt progress. Called once the hunt is fully complete
 * (progressing past PUZZLE_COMPLETE makes this data irrelevant going
 * forward) and also as part of resetDiscovery()'s full wipe.
 */
export function clearCollectedLetters() {
  localStorage.removeItem(STORAGE_KEYS.COLLECTED_LETTERS);
}

/**
 * Returns the current stage. Exposed read-only to stage modules that
 * need to know "where am I" for rendering decisions (e.g. main.js
 * deciding which module to mount on load if a visitor refreshes
 * mid-sequence).
 */
export function getStage() {
  return getCurrentStage();
}

/**
 * Developer/debug utility: fully resets all easter egg state for this
 * browser, as if it had never been visited. NOT called anywhere in the
 * production flow — exposed on window only when needed for manual
 * testing (see Testing Instructions in the final summary).
 */
export function resetDiscovery() {
  localStorage.removeItem(STORAGE_KEYS.DISCOVERY_COMPLETE);
  localStorage.removeItem(STORAGE_KEYS.CURRENT_STAGE);
  localStorage.removeItem(STORAGE_KEYS.COLLECTED_LETTERS);
}
