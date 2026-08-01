/**
 * deathLock.js
 * -----------------------------------------------------------------------
 * Watches for GameState.lockedUntil going from null to set — the
 * signal that the death cycle (5 marks, see handlers/game.go's
 * RecordDeath) just triggered a 24h lockout on THIS attack. The
 * backend enforces the lockout on every future request via a 423
 * (api/client.js already clears the session on that), but the
 * request that TRIGGERS the lockout still comes back as an ordinary
 * 200 — it's the attack response that first carries lockedUntil.
 * Without this check, the player wouldn't see a forced logout until
 * their *next* command happened to bounce off a 423.
 *
 * Only attack.js calls this today, since attack is the only action
 * that can end in defeat. If a future action can also trigger a
 * death, call this from there too — it's cheap and idempotent (a
 * second call after lockedUntil is already set just repeats the
 * logout, harmlessly, since setEnabled(false)/reload only happen
 * once in practice).
 * -----------------------------------------------------------------------
 */

import { clearSession } from '../api/client.js';

/**
 * @param {import('../terminal/TerminalEmulator.js').TerminalEmulator} term
 * @param {import('./GameState.js').GameState} gameState
 * @returns {Promise<boolean>} true if a lockout was found and the
 *   forced-logout sequence was started (caller should stop doing
 *   anything else with `term`/`gameState` after this resolves).
 */
export async function checkAndHandleLockout(term, gameState) {
  if (!gameState.lockedUntil) return false;

  const unlockAt = new Date(gameState.lockedUntil);
  const unlockText = Number.isNaN(unlockAt.getTime())
    ? gameState.lockedUntil
    : unlockAt.toLocaleString();

  term.print('', 'error');
  term.print('Death marks you for the fifth time.', 'error');
  term.print(`You are cast out of the dungeon, locked out until ${unlockText}.`, 'error');
  term.print('Logging out...', 'system');

  // Same "disable input, clear session, reload after a beat" sequence
  // exit.js uses for a normal logout — reusing location.reload() here
  // means main.js's boot() (which re-checks isAuthenticated()) is the
  // one place "back to a clean login screen" is implemented, rather
  // than this file trying to reset terminal/auth state in place.
  term.setEnabled(false);
  clearSession();

  await new Promise((resolve) => setTimeout(resolve, 1200));
  location.reload();

  return true;
}
