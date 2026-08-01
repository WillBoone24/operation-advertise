/**
 * use.js
 * -----------------------------------------------------------------------
 * Consumes a potion from inventory: `use <item_id>`. Mirrors equip.js's
 * shape closely — both act on an Inventory item ID and both let the
 * backend be the one source of truth on whether that ID is valid,
 * rather than pre-validating item IDs client-side. The key difference
 * from equip is that a used potion never goes back into Inventory (see
 * handlers/game.go's handleUse) — it's gone the moment it's drunk.
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';

export const useCommand = {
  name: 'use',
  description: 'Drink a potion from your inventory: use <item_id>',

  async execute({ term, args, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <n> <class> <difficulty>" to begin.', 'error');
      return;
    }

    const [itemId] = args;
    if (!itemId) {
      term.print('Usage: use <item_id>', 'error');
      if (gameState.inventory.length > 0) {
        term.print(`Held: ${gameState.inventory.join(', ')}`);
      } else {
        term.print('Your inventory is empty.');
      }
      return;
    }

    let log;
    try {
      log = await gameState.usePotion(itemId);
    } catch (err) {
      term.print(err.message || 'Could not use that item.', 'error');
      return;
    }

    for (const line of log) term.print(line);
    printStatus(term, gameState);
  },
};
