/**
 * hall.js
 * -----------------------------------------------------------------------
 * The king's chambers — the true end of a run. Only reachable once
 * gameState.atKingsChambers is true, which the backend sets exactly
 * once, on clearing the Journey's Black Mire finale (see
 * handlers/game.go's handleDescend). There is no other way in, and no
 * "leave" — this is the last location a character visits.
 *
 * Two forms, mirroring tavern.js's subcommand shape:
 *   hall                 - shows the Hall of Legacies (everyone who's
 *                           finished before this character) and, if
 *                           this character hasn't chosen yet, the
 *                           three paths on offer
 *   hall choose <path>   - permanently commits to one path
 *                           ("lords" | "commons" | "heroes")
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';

export const hallCommand = {
  name: 'hall',
  description: 'Enter the king\'s chambers (only once your journey is complete).',

  /**
   * @param {Object} ctx
   * @param {import('../../terminal/TerminalEmulator.js').TerminalEmulator} ctx.term
   * @param {string[]} ctx.args
   * @param {import('../GameState.js').GameState} ctx.gameState
   */
  async execute({ term, args, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet.', 'error');
      return;
    }

    // Same reason tavern.js refreshes before checking atTavern: a
    // "descend" that just set atKingsChambers on the server needs to
    // be reflected here before this command can rely on it.
    try {
      await gameState.loadGame();
    } catch (err) {
      term.print(err.message || 'Could not reach the king\'s chambers.', 'error');
      return;
    }

    if (!gameState.atKingsChambers) {
      term.print('There is no such place here.', 'error');
      return;
    }

    let log;
    try {
      if (args[0]?.toLowerCase() === 'choose') {
        const pathId = args[1];
        if (!pathId) {
          term.print('Choose which path? (lords | commons | heroes)', 'error');
          return;
        }
        log = await gameState.choosePath(pathId.toLowerCase());
      } else {
        log = await gameState.legacyHall();
      }
    } catch (err) {
      // A 409 here almost always means "you've already chosen" —
      // a legitimate outcome worth showing plainly, not a bug.
      term.print(err.message || 'That failed.', 'error');
      return;
    }

    for (const line of log) {
      // legacyHall's server response includes a blank "" line to
      // separate the Hall of Legacies from the path menu — print()
      // handles an empty string fine as a blank line, same as
      // combat/tavern narration already relies on elsewhere.
      term.print(line, 'system');
    }
    printStatus(term, gameState);
  },
};
