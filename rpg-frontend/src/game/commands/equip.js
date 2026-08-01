/**
 * equip.js
 * -----------------------------------------------------------------------
 * Equips an armor piece from inventory: `equip <item_id>`. Weapons are
 * never equip-able post-creation (see items.go's design note on
 * class-restricted weapons) — attempting to equip anything else,
 * including a weapon ID, is rejected by the backend with a clear error
 * message, which this command just surfaces as-is rather than trying
 * to pre-validate item IDs itself.
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';

export const equipCommand = {
  name: 'equip',
  description: 'Equip an armor piece from your inventory: equip <item_id>',

  async execute({ term, args, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <name> <class> <difficulty>" to begin.', 'error');
      return;
    }

    const [itemId] = args;
    if (!itemId) {
      term.print('Usage: equip <item_id>', 'error');
      if (gameState.inventory.length > 0) {
        term.print(`Held: ${gameState.inventory.join(', ')}`);
      } else {
        term.print('Your inventory is empty.');
      }
      return;
    }

    let log;
    try {
      log = await gameState.equip(itemId);
    } catch (err) {
      term.print(err.message || 'Could not equip that item.', 'error');
      return;
    }

    for (const line of log) term.print(line);
    printStatus(term, gameState);
  },
};