/**
 * main.js
 * -----------------------------------------------------------------------
 * Orchestrator for the easter egg sequence. This is the ONLY module that
 * imports all three stage modules and decides which one to mount — the
 * stage modules themselves never import each other, which keeps them
 * independently testable and keeps the actual sequencing logic in one
 * readable place instead of scattered across files.
 *
 * Responsibilities:
 *   1. On EVERY page load — a fresh visit or a plain reload — wipe any
 *      stored discovery/stage/letter progress first, so the sequence
 *      always starts over from IDLE. This intentionally replaces the
 *      original "one-time per browser" behavior: the easter egg is no
 *      longer sticky across reloads, it's re-triggerable every visit.
 *   2. Look at the (now-reset) current stage and mount the ONE module
 *      that corresponds to it. In practice this is always IDLE now,
 *      since step 1 just cleared everything, but the switch is kept in
 *      case that reset is ever made conditional again.
 *   3. Wire each stage's completion callback to advance state and mount
 *      the next stage.
 * -----------------------------------------------------------------------
 */

import { STAGES, getStage, advanceStage, resetDiscovery } from './state.js';
import { mountBouncingLogo } from './bouncing-logo.js';
import { mountLetterPuzzle } from './letter-puzzle.js';
import { mountRegistration } from './registration.js';

function init() {
  // Every page load starts the easter egg fresh — clear whatever
  // discovery/stage/letter progress a previous visit (or an earlier
  // reload in this same session) left behind. This is what makes the
  // sequence replay each time instead of permanently disappearing
  // after the first successful run in a given browser.
  resetDiscovery();

  const stage = getStage();

  switch (stage) {
    case STAGES.IDLE:
      startStage1();
      break;
    case STAGES.LOGO_CLICKED:
      // Visitor clicked the logo in a previous page load (e.g.
      // refreshed mid-puzzle) — skip straight to the puzzle rather
      // than showing the bouncing logo again.
      startStage2();
      break;
    case STAGES.PUZZLE_COMPLETE:
      // Same idea: puzzle was already solved, resume at registration.
      startStage3();
      break;
    case STAGES.REGISTERED:
      // Reachable only if REGISTERED was set without discovery being
      // marked complete, which registration.js never does (it always
      // sets both together). Doing nothing here is the safe default
      // rather than guessing at recovery behavior for a state that
      // shouldn't exist.
      break;
    default:
      // Unrecognized stage value — state.js already defends against
      // this at the storage layer (falls back to IDLE), so this
      // branch should be unreachable. No-op rather than throwing.
      break;
  }
}

function startStage1() {
  mountBouncingLogo({
    onClicked: () => {
      advanceStage(STAGES.LOGO_CLICKED);
      startStage2();
    },
  });
}

function startStage2() {
  mountLetterPuzzle({
    onComplete: () => {
      advanceStage(STAGES.PUZZLE_COMPLETE);
      startStage3();
    },
  });
}

function startStage3() {
  mountRegistration({
    onComplete: () => {
      // registration.js has already called advanceStage(REGISTERED)
      // and markDiscoveryComplete() internally by the time this fires
      // (see its handleSubmit success path) — this callback only
      // needs to clean up the visible overlay so the visitor lands
      // back on the normal site.
      document.querySelectorAll('.ee-overlay').forEach((el) => el.remove());
    },
  });
}

if (document.readyState === 'loading') {
  document.addEventListener('DOMContentLoaded', init);
} else {
  init();
}

// ---------------------------------------------------------------------
// Manual testing hook — NOT part of the production flow. Never called
// automatically anywhere in this codebase. See the testing
// instructions for how to use this from the browser console.
// ---------------------------------------------------------------------
window.__resetEasterEgg = () => {
  resetDiscovery();
  location.reload();
};