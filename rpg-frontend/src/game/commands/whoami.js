/**
 * whoami.js
 * -----------------------------------------------------------------------
 * Prints the logged-in player's profile, sourced from GameState (which
 * itself mirrors GET /api/me — see GameState.js's doc comment on why
 * that data isn't authoritative, just a local display copy).
 *
 * COMMAND OBJECT CONVENTION — established here for future commands
 * (help.js, status.js, and eventually real game actions) to follow
 * consistently:
 *
 *   {
 *     name: string,          // the word typed to invoke it, e.g. "whoami"
 *     description: string,   // one line, shown by the future help command
 *     async execute(ctx)      // ctx = { term, args, gameState }
 *   }
 *
 * A shared shape like this is what lets a future command registry (in
 * main.js) dispatch on `parsedCommand.command` generically — look the
 * name up in a map of these objects and call `.execute(ctx)` — rather
 * than hand-writing an if/else chain that grows unwieldy as commands
 * are added. It's also what lets a future help.js enumerate every
 * registered command's `name` + `description` automatically instead of
 * maintaining a separate hardcoded list that can drift out of sync.
 * -----------------------------------------------------------------------
 */

export const whoamiCommand = {
  name: 'whoami',
  description: 'Show your account details.',

  /**
   * @param {Object} ctx
   * @param {import('../../terminal/TerminalEmulator.js').TerminalEmulator} ctx.term
   * @param {string[]} ctx.args - unused by this command; whoami takes
   *   no arguments, but the parameter is part of the shared ctx shape
   *   every command receives regardless of whether it needs it.
   * @param {import('../GameState.js').GameState} ctx.gameState
   */
  async execute({ term, gameState }) {
    if (!gameState.isLoaded) {
      // Shouldn't normally happen — main.js is expected to load()
      // GameState right after login, before any command can run —
      // but this is cheap insurance against a future ordering bug
      // rather than printing undefined/null fields silently.
      term.print('Player data has not loaded yet. Try again shortly.', 'error');
      return;
    }

    term.print(`username        ${gameState.username}`);
    term.print(`user_id         ${gameState.userId}`);
    term.print(`level           ${gameState.level}`);
    term.print(`story_completed ${gameState.storyCompleted}`);
    term.print(`easter_egg      ${gameState.easterEggFound}`);
  },
};