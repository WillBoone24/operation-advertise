/**
 * inspect.js
 * -----------------------------------------------------------------------
 * `inspect <spell_id>` — shows a spell's real mechanical detail (damage
 * dice or heal %, whether it auto-hits, mana cost) plus its flavor
 * description. Works at any time, not just in combat or the tavern —
 * it only ever reads gameState.spells (the character's known spells:
 * MageSpells' starting kit + any learned tavern.js scrolls) and
 * gameState.tavernSpells (the two scrolls currently for sale, if
 * atTavern), both of which the backend already resolves with full
 * detail (see handlers/game.go's newSpellResponse) — no separate
 * lookup endpoint needed.
 *
 * `inspect` with no args lists everything currently inspectable,
 * rather than silently doing nothing — same "no-arg call shows a
 * menu" convention tavern.js's bare `tavern` follows.
 * -----------------------------------------------------------------------
 */

function formatEffect(spell) {
  if (spell.kind === 'damage') {
    const hitNote = spell.autoHit
      ? 'always hits, ignoring the enemy\'s defenses'
      : 'requires a normal hit roll against the enemy\'s defenses';
    return `Damage: ${spell.damageDieCount}d${spell.damageDieSides} (${hitNote})`;
  }
  if (spell.kind === 'heal') {
    return `Heals ${spell.healPercentOfMaxHP}% of your max HP`;
  }
  return 'Effect: unknown';
}

/**
 * Looks up a spell ID against everything currently inspectable:
 * gameState.spells first (already known — can be cast right now), then
 * gameState.tavernSpells (currently offered but not yet learned, only
 * non-empty while atTavern). Checking known spells first means a
 * spell that's BOTH previously learned and (by chance) not currently
 * re-offered still resolves to the "known" entry rather than nothing.
 */
function findSpell(gameState, spellId) {
  const known = gameState.spells.find((s) => s.id === spellId);
  if (known) {
    return { spell: known, known: true, price: null };
  }
  const offered = gameState.tavernSpells.find((s) => s.id === spellId);
  if (offered) {
    return { spell: offered, known: false, price: offered.price };
  }
  return null;
}

function printSpell(term, entry) {
  const { spell, known, price } = entry;
  term.print(`${spell.name} (${spell.id})`, 'system');
  term.print(`  ${spell.description}`);
  term.print(`  Mana cost: ${spell.manaCost}`);
  term.print(`  ${formatEffect(spell)}`);
  if (!known) {
    term.print(`  Not yet learned — ${price} gold at the tavern ("tavern buy ${spell.id}")`);
  }
}

export const inspectCommand = {
  name: 'inspect',
  description: 'Inspect a spell\'s damage, effect, and description: inspect [spell_id]',

  async execute({ term, args, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <n> <class> <difficulty>" to begin.', 'error');
      return;
    }

    // Refresh first, same reason inventory.js/look.js do — a spell
    // just learned, or a tavern just entered/re-rolled, should be
    // reflected immediately rather than showing stale local state.
    try {
      await gameState.loadGame();
    } catch (err) {
      term.print(err.message || 'Could not inspect right now.', 'error');
      return;
    }

    const [spellId] = args;

    // Union of known spells and currently-offered tavern scrolls, de-
    // duplicated by ID (a scroll re-offered after already being
    // learned should only show up once, as the "known" entry).
    const inspectable = [
      ...gameState.spells,
      ...gameState.tavernSpells.filter((s) => !gameState.spells.some((k) => k.id === s.id)),
    ];

    if (!spellId) {
      if (inspectable.length === 0) {
        term.print('There is nothing to inspect right now.', 'system');
        return;
      }
      term.print('Spells you can inspect:', 'system');
      for (const spell of inspectable) {
        term.print(`  ${spell.id} — ${spell.name}`);
      }
      term.print('Type "inspect <spell_id>" for full detail.', 'system');
      return;
    }

    const entry = findSpell(gameState, spellId);
    if (!entry) {
      term.print(`Unknown spell: "${spellId}". Type "inspect" to see what you can look up.`, 'error');
      return;
    }
    printSpell(term, entry);
  },
};
