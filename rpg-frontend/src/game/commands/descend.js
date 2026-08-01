/**
 * descend.js
 * -----------------------------------------------------------------------
 * Advances past a cleared encounter: `descend`. Only valid once the
 * current encounter has actually been cleared (gameState.pendingAdvance)
 * — see state.go's PendingAdvance doc comment for why that flag exists
 * at all, and handlers/game.go's handleDescend for the server-side
 * enforcement this command's pre-check mirrors (but doesn't replace:
 * the backend still rejects an invalid descend on its own).
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';

export const descendCommand = {
  name: 'descend',
  description: 'Advance to the next encounter once the current one is cleared.',

  async execute({ term, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <name> <class> <difficulty>" to begin.', 'error');
      return;
    }
    if (!gameState.pendingAdvance) {
      term.print('There is nothing to descend past yet. Try "look".', 'error');
      return;
    }

    let log;
    try {
      log = await gameState.descend();
    } catch (err) {
      term.print(err.message || 'Could not descend.', 'error');
      return;
    }

    for (const line of log ?? []) term.print(line);
    printStatus(term, gameState);

    if (gameState.atTavern) {
      term.print('Type "tavern" to see what you can do here.', 'system');
    } else if (!gameState.runComplete) {
      term.print('Type "look" to see what is ahead.', 'system');
    }
  },
};