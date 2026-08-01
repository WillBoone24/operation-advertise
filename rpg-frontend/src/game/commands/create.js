/**
 * create.js
 * -----------------------------------------------------------------------
 * Creates a new character: `create <name> <class> <difficulty>`.
 *
 * Deliberately does NOT hardcode the list of valid classes/difficulties
 * here — internal/game/classes.go on the backend is the single source
 * of truth for what's valid, and NewSaveState's validation already
 * produces a clear "unknown class %q" / "unknown difficulty %q" error
 * message. Duplicating that list here would just be a second place it
 * could drift out of sync with the backend. If the arguments are wrong,
 * the backend's own error message is shown as-is.
 * -----------------------------------------------------------------------
 */

import { printStatus } from '../statusLine.js';

export const createCommand = {
  name: 'create',
  description: 'Create a character: create <name> <class> <difficulty>',

  async execute({ term, args, gameState }) {
    if (gameState.hasCharacter) {
      term.print('You already have a character. There is no "start over" yet.', 'error');
      return;
    }

    const [characterName, className, difficulty] = args;
    if (!characterName || !className || !difficulty) {
      term.print('Usage: create <name> <class> <difficulty>', 'error');
      term.print('  e.g. create Aldric fighter hard');
      return;
    }

    let log;
    try {
      log = await gameState.createCharacter(characterName, className, difficulty);
    } catch (err) {
      // Surfaces the backend's own validation message directly (e.g.
      // "unknown class \"wizzard\"") — see this file's doc comment on
      // why the valid-values list isn't duplicated here.
      term.print(err.message || 'Failed to create character.', 'error');
      return;
    }

    for (const line of log) term.print(line);
    term.print('Character created. Type "look" to see what is ahead.', 'system');
    printStatus(term, gameState);
  },
};