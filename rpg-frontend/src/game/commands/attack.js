/**
 * attack.js
 * -----------------------------------------------------------------------
 * Resolves one attack: `attack`. Takes no arguments — GameState.attack()
 * (and the backend's POST /api/game/action handler behind it) figures
 * out on its own whether this starts a new fight or continues one
 * already in progress. See handlers/game.go's handleAttack doc comment.
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';
import { checkAndHandleLockout } from '../deathLock.js';

export const attackCommand = {
  name: 'attack',
  description: 'Attack the enemy in front of you.',

  async execute({ term, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <name> <class> <difficulty>" to begin.', 'error');
      return;
    }
    if (gameState.runComplete) {
      term.print('There is nothing left to fight. Your run is complete.', 'error');
      return;
    }
    if (gameState.atTavern) {
      term.print('You are in the tavern. Type "tavern leave" before fighting again.', 'error');
      return;
    }
    if (gameState.pendingAdvance) {
      term.print('This encounter is already cleared. Type "descend" to continue.', 'error');
      return;
    }

    let log;
    try {
      log = await gameState.attack();
    } catch (err) {
      term.print(err.message || 'The attack failed.', 'error');
      return;
    }

    for (const line of log) term.print(line);

    // A fresh lockout means this attack was the 5th death mark — bail
    // out before printStatus, there's no character state left worth
    // showing once the forced-logout sequence starts.
    if (await checkAndHandleLockout(term, gameState)) return;

    printStatus(term, gameState);
  },
};