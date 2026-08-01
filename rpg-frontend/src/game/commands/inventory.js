/**
 * inventory.js
 * -----------------------------------------------------------------------
 * Lists held-but-not-equipped items and what's currently equipped:
 * `inventory`. Refreshes from the backend first (see look.js's doc
 * comment on the same choice) so a reward just granted by "descend"
 * always shows up even if something else changed state in between.
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';

export const inventoryCommand = {
  name: 'inventory',
  description: 'List your equipped gear and held items.',

  async execute({ term, gameState }) {
    try {
      await gameState.loadGame();
    } catch (err) {
      term.print(err.message || 'Could not check your inventory.', 'error');
      return;
    }

    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <name> <class> <difficulty>" to begin.', 'error');
      return;
    }

    printStatus(term, gameState);
    term.print('', 'system');

    term.print('Equipped:', 'system');
    term.print(`  Weapon: ${gameState.equipped.weaponId}`);
    term.print(`  Armor:  ${gameState.equipped.armorId || '(none)'}`);

    term.print('Inventory:', 'system');
    if (gameState.inventory.length === 0) {
      term.print('  (empty)');
    } else {
      for (const itemId of gameState.inventory) {
        term.print(`  ${itemId}`);
      }
      term.print('Type "equip <item_id>" to wear armor, or "use <item_id>" to drink a potion.');
    }

    if (gameState.class === 'mage' && gameState.spells.length > 0) {
      term.print('', 'system');
      term.print(`Spells (Mana ${gameState.mana}):`, 'system');
      for (const spell of gameState.spells) {
        term.print(`  ${spell.id} — ${spell.name} (${spell.manaCost} mana)`);
      }
      term.print('Type "cast <spell_id>" to cast one.');
    }

    // Fighter/Cleric/Ranger's once-per-stage ability, mirroring the
    // Mage spells section above. Rogue's Sneak Attack is passive (no
    // "ability" action exists for it) and Mage's Firebolt is already
    // covered by the spells section, so neither shows up here.
    if (gameState.ability && gameState.class !== 'mage' && gameState.class !== 'rogue') {
      term.print('', 'system');
      const availability = gameState.ability.usable ? 'available' : 'used this stage';
      term.print(`Ability: ${gameState.ability.name} (${availability})`, 'system');
      term.print(`  ${gameState.ability.description}`);
      term.print('Type "ability" to use it.');
    }

    // One-line mention only — the full name/description lives in the
    // dedicated "familiar" command (see familiar.js's doc comment on
    // that split), same as spells getting a one-line-per-spell listing
    // here but their own no-arg "cast" view for anything more.
    if (gameState.familiar) {
      term.print('', 'system');
      term.print(`Familiar: ${gameState.familiar.name} (type "familiar" for details)`, 'system');
    }

    // Only ever populated once the player has paid to learn it in the
    // tavern (see GameState._applyGameState's doc comment on
    // monsterLore) — shown here on every inventory check from then on,
    // per the "listed whenever inventory is called after the tavern"
    // requirement this was built for.
    if (gameState.monsterLore) {
      term.print('', 'system');
      term.print('Monster lore:', 'system');
      for (const line of gameState.monsterLore) {
        term.print(`  ${line}`);
      }
    }
  },
};