/**
 * ability.js
 * -----------------------------------------------------------------------
 * Uses the character's class ability: `ability`, no arguments — every
 * class has exactly one (see classes.go), so unlike cast.js there's no
 * ID to choose between.
 *
 * Only Fighter (Second Wind), Cleric (Mend), and Ranger (Steady Aim)
 * have an ability usable this way. The backend rejects it outright for
 * Mage (use "cast" instead) and Rogue (Sneak Attack is passive — it
 * triggers on its own during "attack", see handlers/game.go's
 * FirstAttackLanded check), but this checks gameState.ability.usable
 * first anyway so those two classes get an immediate, friendly answer
 * instead of a round trip to the server just to be told no — same
 * "pre-check for a fast local answer, let the backend be the real
 * authority" convention cast.js follows for its own class gate.
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';
import { checkAndHandleLockout } from '../deathLock.js';

export const abilityCommand = {
  name: 'ability',
  description: 'Use your class ability (once per stage): ability',

  async execute({ term, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <n> <class> <difficulty>" to begin.', 'error');
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

    if (!gameState.ability) {
      term.print('You have no ability to use.', 'error');
      return;
    }
    if (gameState.class === 'mage') {
      term.print('A Mage has no ability action — type "cast" to cast a spell instead.', 'error');
      return;
    }
    if (gameState.class === 'rogue') {
      term.print(`${gameState.ability.name} is passive — it triggers on its own the first time you land an attack this fight.`, 'error');
      return;
    }
    if (!gameState.ability.usable) {
      term.print(`You've already used ${gameState.ability.name} this stage.`, 'error');
      return;
    }

    let log;
    try {
      log = await gameState.useAbility();
    } catch (err) {
      term.print(err.message || 'That failed.', 'error');
      return;
    }

    for (const line of log) term.print(line);

    // A fresh lockout means this ability's counterattack triggered the
    // 5th death mark — bail out before printStatus, same rule attack.js
    // and cast.js follow.
    if (await checkAndHandleLockout(term, gameState)) return;

    printStatus(term, gameState);
  },
};