/**
 * look.js
 * -----------------------------------------------------------------------
 * Describes what's immediately in front of the player: the current
 * encounter's flavor text, or the enemy they're mid-fight with if
 * combat is already in progress. Refreshes from the backend first via
 * gameState.loadGame() (GameState is a dumb mirror — see GameState.js's
 * doc comment) rather than trusting whatever it already holds, since
 * another action could have changed things since the last render.
 *
 * Leads with printStatus() (see statusLine.js) as the same dashboard
 * every action command shows — look isn't a special case with its own
 * separate HP/enemy readout, it's just the dashboard plus narrative
 * flavor text on top.
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';

export const lookCommand = {
  name: 'look',
  description: 'Describe your current surroundings.',

  async execute({ term, gameState }) {
    try {
      await gameState.loadGame();
    } catch (err) {
      term.print(err.message || 'Could not check your surroundings.', 'error');
      return;
    }

    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <name> <class> <difficulty>" to begin.', 'error');
      return;
    }

    printStatus(term, gameState);

    if (gameState.runComplete) {
      return;
    }

    if (gameState.atTavern) {
      term.print('Firelight, worn wooden tables, and a board covered in other travelers\' notes.');
      return;
    }

    if (gameState.inCombat) {
      term.print('Type "attack" to strike.');
      if (gameState.class === 'mage') {
        term.print('Type "cast <spell_id>" to cast a spell instead.');
      }
      return;
    }

    if (gameState.pendingAdvance) {
      return;
    }

    const enc = gameState.encounter;
    if (enc) {
      const stageLabel = gameState.currentStage <= 5
        ? `Dungeon Stage ${gameState.currentStage}/5`
        : `Journey, Stage ${gameState.currentStage - 5}/5`;
      term.print(`${enc.description} (${stageLabel})`);
      if (enc.isStageFinale) {
        term.print('This feels like it might be the hardest fight of the stage.', 'system');
      }
      term.print('Type "attack" to engage.');
    }
  },
};