/**
 * tavern.js
 * -----------------------------------------------------------------------
 * The tavern's front door: `tavern` with no args shows the menu (and
 * refreshes state first, same reason look.js/inventory.js do — a
 * "descend" that just triggered tavern entry should be reflected
 * immediately). Subcommands:
 *   tavern lore              - learn spell-effectiveness lore (free)
 *   tavern buy <item_id>     - buy a tonic or spell scroll with gold
 *   tavern riddle [answer]   - ask for, or answer, the tavern's riddle
 *   tavern blackjack <amt>   - start a round, wagering <amt> gold
 *   tavern blackjack hit     - take another card in the current round
 *   tavern blackjack stand   - stand and let the dealer play out
 *   tavern roulette <amt> <bet> - spin, wagering <amt> gold on <bet>
 *                              ("red"/"black"/"odd"/"even"/a number 0-36)
 *   tavern leave             - head back into the dungeon (Stage 3+)
 *   tavern exit              - begin the Journey home (only once
 *                              dungeonComplete — i.e. the Stage 5
 *                              waypoint tavern, not the Stage 2 one)
 *
 * POTION_ITEMS below is display-only content — the backend
 * (game/tavern.go's TavernPotions) is the actual source of truth on
 * what's purchasable and at what price; this list must be kept in
 * sync with it by hand, same "static content, no live catalog
 * endpoint" tradeoff cast.js/inventory.js already accept for item IDs
 * elsewhere in this frontend.
 *
 * Unlike potions, the two scroll spells on offer are NOT static —
 * game/tavern.go's ScrollSpells is a 7-spell pool the backend rolls 2
 * from on every tavern visit (see game.RollTavernSpells), so they're
 * rendered straight from gameState.tavernSpells (the backend's actual
 * pick for THIS visit) instead of a hand-maintained list here. Use
 * "inspect <spell_id>" to see a scroll's full damage/heal detail
 * before buying it.
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';

const POTION_ITEMS = [
  { id: 'p_stat_tonic', price: 1, note: 'tonic — permanently raises a random stat' },
  { id: 'p_hp_elixir', price: 2, note: 'tonic — full HP restore' },
];

function printMenu(term, gameState) {
  term.print('You are in the tavern.', 'system');
  if (gameState.blackjackActive) {
    term.print(`You're mid-hand at blackjack (wagered ${gameState.blackjackWager} gold).`, 'system');
    term.print(`  Your hand: ${gameState.blackjackPlayerCards.join(', ')}`, 'system');
    term.print(`  Dealer shows: ${gameState.blackjackDealerCards[0]}, ?`, 'system');
    term.print('  Type "tavern blackjack hit" or "tavern blackjack stand".', 'system');
    term.print('', 'system');
  }
  term.print('  tavern lore            - ask about monster weaknesses (free)', 'system');
  term.print('  tavern buy <item_id>   - buy a tonic or spell scroll', 'system');
  term.print('  inspect <spell_id>     - see a spell\'s damage/effect detail', 'system');
  term.print('  tavern riddle          - hear the tavern-keeper\'s riddle', 'system');
  term.print(`  tavern blackjack <amt> - play blackjack, 1-10 gold a round (${gameState.blackjackRoundsPlayed}/5 played this run)`, 'system');
  term.print(`  tavern roulette <amt> <bet> - spin the wheel, 1-10 gold a spin (${gameState.rouletteRoundsPlayed}/5 played this run)`, 'system');
  term.print('    bets: red, black, odd, even, or a number 0-36 (35:1)', 'system');
  term.print('  board                  - read the community board', 'system');
  term.print('  note "<message>"       - leave a note on the community board', 'system');
  term.print('  pin "<message>"        - leave a PERMANENT note for 4 gold', 'system');
  if (!gameState.runComplete) {
    if (gameState.dungeonComplete) {
      term.print('  tavern exit            - begin the journey home', 'system');
    } else {
      term.print('  tavern leave           - head back down to Stage 3', 'system');
    }
  }
  term.print('', 'system');
  term.print('For sale:', 'system');
  for (const item of POTION_ITEMS) {
    term.print(`  ${item.id} — ${item.price} gold — ${item.note}`);
  }
  if (gameState.tavernSpells.length === 0) {
    term.print('  (no spell scrolls on offer this visit)');
  } else {
    for (const spell of gameState.tavernSpells) {
      const tag = gameState.class === 'mage' ? '' : ' (Mage only)';
      term.print(`  ${spell.id} — ${spell.price} gold — spell scroll${tag}: ${spell.name}`);
    }
    term.print('Type "inspect <spell_id>" for a scroll\'s full detail before buying.', 'system');
  }
}

export const tavernCommand = {
  name: 'tavern',
  description: 'Interact with the tavern: tavern [lore|buy <item_id>|riddle [answer]|blackjack <amt>|blackjack hit|blackjack stand|roulette <amt> <bet>|leave]',

  async execute({ term, args, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <n> <class> <difficulty>" to begin.', 'error');
      return;
    }

    try {
      await gameState.loadGame();
    } catch (err) {
      term.print(err.message || 'Could not reach the tavern.', 'error');
      return;
    }

    if (!gameState.atTavern) {
      term.print('There is no tavern here right now.', 'error');
      return;
    }

    const [sub, ...rest] = args;

    if (!sub) {
      printMenu(term, gameState);
      return;
    }

    let log;
    try {
      switch (sub.toLowerCase()) {
        case 'lore':
          log = await gameState.tavernLore();
          break;
        case 'buy': {
          const [itemId] = rest;
          if (!itemId) {
            term.print('Usage: tavern buy <item_id>', 'error');
            return;
          }
          log = await gameState.tavernBuy(itemId);
          break;
        }
        case 'riddle':
          log = await gameState.tavernRiddle(rest.join(' ') || undefined);
          break;
        case 'blackjack': {
          const [first] = rest;
          const firstLower = (first || '').toLowerCase();
          if (firstLower === 'hit') {
            log = await gameState.tavernBlackjackHit();
          } else if (firstLower === 'stand') {
            log = await gameState.tavernBlackjackStand();
          } else if (!first && gameState.blackjackActive) {
            // No amount given, but a round's already in progress — the
            // backend ignores the (missing) amount once BlackjackActive
            // is true and just re-shows the hand, same as tavern
            // riddle's no-answer re-show.
            log = await gameState.tavernBlackjackStart(0);
          } else {
            const amount = Number(first);
            if (!first || !Number.isInteger(amount) || amount <= 0) {
              term.print('Usage: tavern blackjack <amount> | hit | stand', 'error');
              return;
            }
            log = await gameState.tavernBlackjackStart(amount);
          }
          break;
        }
        case 'roulette': {
          const [amountStr, bet] = rest;
          const amount = Number(amountStr);
          if (!amountStr || !Number.isInteger(amount) || amount <= 0 || !bet) {
            term.print('Usage: tavern roulette <amount> <red|black|odd|even|0-36>', 'error');
            return;
          }
          log = await gameState.tavernRoulette(bet, amount);
          break;
        }
        case 'leave':
          log = await gameState.tavernLeave();
          break;
        case 'exit':
          log = await gameState.exitDungeon();
          break;
        default:
          term.print(`Unknown tavern command: "${sub}". Type "tavern" for the menu.`, 'error');
          return;
      }
    } catch (err) {
      term.print(err.message || 'The tavern-keeper shakes their head.', 'error');
      return;
    }

    for (const line of log) term.print(line);
    printStatus(term, gameState);
  },
};