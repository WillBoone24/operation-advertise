/**
 * cast.js
 * -----------------------------------------------------------------------
 * Casts one of the Mage's permanent spells: `cast <spell_id>`. Mage-only
 * — the backend rejects it for every other class (handlers/game.go's
 * handleCast), but this checks gameState.class first anyway so a
 * non-Mage gets an immediate, friendly answer instead of a round trip
 * to the server just to be told no.
 *
 * With no argument, lists the caster's known spells (gameState.spells,
 * populated from GET /api/game/state — see GameState.js's doc comment
 * on mana/spells being Mage-only fields) alongside their mana cost,
 * the same "no args -> show what you can act on" convention equip.js
 * and use.js follow for inventory.
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';
import { checkAndHandleLockout } from '../deathLock.js';

export const castCommand = {
  name: 'cast',
  description: 'Cast a known spell (Mage only): cast <spell_id>',

  async execute({ term, args, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <n> <class> <difficulty>" to begin.', 'error');
      return;
    }
    if (gameState.class !== 'mage') {
      term.print('Only a Mage can cast spells.', 'error');
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

    const [spellId] = args;
    if (!spellId) {
      term.print('Usage: cast <spell_id>', 'error');
      term.print(`Mana: ${gameState.mana}`, 'system');
      if (gameState.spells.length > 0) {
        term.print('Known spells:', 'system');
        for (const spell of gameState.spells) {
          term.print(`  ${spell.id} — ${spell.name} (${spell.manaCost} mana): ${spell.description}`);
        }
      }
      return;
    }

    let log;
    try {
      log = await gameState.castSpell(spellId);
    } catch (err) {
      term.print(err.message || 'The spell failed.', 'error');
      return;
    }

    for (const line of log) term.print(line);

    // A fresh lockout means this cast triggered the 5th death mark —
    // bail out before printStatus, same rule attack.js follows.
    if (await checkAndHandleLockout(term, gameState)) return;

    printStatus(term, gameState);
  },
};
