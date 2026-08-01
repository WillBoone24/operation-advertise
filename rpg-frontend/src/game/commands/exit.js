/**
 * exit.js
 * -----------------------------------------------------------------------
 * Logs the player out and returns the window to its initial (pre-login)
 * state — same shape as whoami.js/help.js/status.js: a plain command
 * object with { name, description, execute(ctx) }.
 *
 * HONEST NOTE ON "SAVING DATA": per status.js's doc comment, the
 * backend has no game engine yet — there is no SaveData endpoint, and
 * models.User.SaveData is an unparsed, never-written placeholder (see
 * backend/internal/models/user.go). There is currently nothing
 * client-authored to persist beyond what the backend already owns
 * (level, story_completed), which live server-side and don't need a
 * client-side "save" step. So this command does NOT pretend to save
 * game state that doesn't exist yet — it tells the player that
 * plainly, then logs out. Once real game state exists client-side,
 * whatever command mutates it should call a real save endpoint before
 * exit.js runs, and this comment should be updated to say so.
 *
 * "Return to the initial state of the window" is implemented as a
 * literal page reload rather than manually resetting terminal/auth
 * state in place — a reload is what main.js's boot() already treats
 * as "initial state" (fresh TerminalEmulator, fresh isAuthenticated()
 * check), so reusing it here avoids maintaining a second, parallel
 * reset path that could drift out of sync with boot()'s real one.
 * -----------------------------------------------------------------------
 */

import { clearSession } from '../../api/client.js';

export const exitCommand = {
  name: 'exit',
  description: 'Log out and return to the login screen.',

  /**
   * @param {Object} ctx
   * @param {import('../../terminal/TerminalEmulator.js').TerminalEmulator} ctx.term
   */
  async execute({ term }) {
    term.print('No unsaved game data exists yet — the world engine', 'system');
    term.print('isn\'t live, so there\'s nothing beyond your account', 'system');
    term.print('state to save. Logging out...', 'system');

    // Disable input immediately so a stray keystroke during the brief
    // window before reload can't be echoed into a terminal that's
    // about to disappear.
    term.setEnabled(false);

    clearSession();

    // A short delay so the message above is actually readable before
    // the page resets, rather than reloading instantly out from under
    // it.
    setTimeout(() => {
      location.reload();
    }, 600);
  },
};