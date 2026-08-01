/**
 * familiar.js
 * -----------------------------------------------------------------------
 * Shows the bonded familiar's full name + description: `familiar`. The
 * persistent status line (statusLine.js) only ever prints the name as
 * an ambient reminder — this is where the flavor text actually lives,
 * same "status line is the summary, a dedicated command is the detail
 * view" split cast.js's no-arg branch and inventory.js already follow
 * for spells/gear.
 *
 * Refreshes from the backend first (see look.js's doc comment on the
 * same choice) since a familiar can appear mid-session, right after a
 * kill, without the player having run any other state-refreshing
 * command since.
 * -----------------------------------------------------------------------
 */

export const familiarCommand = {
  name: 'familiar',
  description: 'Show your bonded familiar, if you have one.',

  async execute({ term, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <name> <class> <difficulty>" to begin.', 'error');
      return;
    }

    try {
      await gameState.loadGame();
    } catch (err) {
      term.print(err.message || 'Could not check your familiar.', 'error');
      return;
    }

    if (!gameState.familiar && !gameState.secondFamiliar) {
      // Deliberately vague about WHEN one might show up (no stage
      // number, no drop chance) — same "don't leak backend tuning
      // numbers into flavor text" restraint the rest of this frontend
      // shows for e.g. gold/poison chances, so this reads as
      // in-fiction uncertainty rather than a spoiled drop table.
      term.print('You have no familiar. Something may yet find you, deeper in the dungeon.', 'system');
      return;
    }

    if (gameState.familiar) {
      term.print(`${gameState.familiar.name}`, 'system');
      term.print(gameState.familiar.description);
    }
    if (gameState.secondFamiliar) {
      term.print(`${gameState.secondFamiliar.name}`, 'system');
      term.print(gameState.secondFamiliar.description);
    }
    term.print('Each fights alongside you every round, automatically — nothing to type.', 'system');
  },
};
