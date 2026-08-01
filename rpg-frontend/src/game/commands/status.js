/**
 * status.js
 * -----------------------------------------------------------------------
 * Reports the actual current state of the player's character: HP,
 * STR/DEX/CON, dungeon progress, and death marks toward the 24h
 * lockout (see handlers/game.go's RecordDeath). This replaces an
 * earlier Phase-1 stub that predated the game engine and always
 * printed "World engine: offline" regardless of what was actually
 * going on — now that there's a real character to report on, this
 * command reports on it, reusing statusLine.js's printStatus() rather
 * than re-deriving the same HP/stats/marks line a second way.
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';

export const statusCommand = {
  name: 'status',
  description: "Show your character's current stats and standing.",

  /**
   * @param {Object} ctx
   * @param {import('../../terminal/TerminalEmulator.js').TerminalEmulator} ctx.term
   * @param {import('../GameState.js').GameState} ctx.gameState
   */
  async execute({ term, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <name> <class> <difficulty>" to begin.', 'error');
      return;
    }

    term.print(`${gameState.characterName} — ${gameState.class} (${gameState.difficulty})`, 'system');
    printStatus(term, gameState);
  },
};
