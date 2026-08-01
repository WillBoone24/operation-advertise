/**
 * help.js
 * -----------------------------------------------------------------------
 * Lists every registered command's name + description.
 *
 * DELIBERATELY does not import a command registry module directly.
 * main.js is what builds the registry (it has to — it's the only place
 * that imports every command file), and if help.js also imported that
 * same registry, you'd get main.js -> help.js -> registry -> (back to)
 * main.js, a circular import. Instead, the full registry is passed in
 * via `ctx.commands` (see main.js), extending the shared command ctx
 * shape whoami.js established: { term, args, gameState, commands }.
 * Every command receives `commands` whether it needs it or not, same
 * as every command receives `args` whether it needs it or not — a
 * consistent ctx shape is worth more than trimming unused fields
 * per-command.
 * -----------------------------------------------------------------------
 */

export const helpCommand = {
  name: 'help',
  description: 'List available commands.',

  /**
   * @param {Object} ctx
   * @param {import('../../terminal/TerminalEmulator.js').TerminalEmulator} ctx.term
   * @param {Record<string, {name: string, description: string}>} ctx.commands -
   *   the full registry, keyed by command name.
   */
  async execute({ term, commands }) {
    const names = Object.keys(commands).sort();
    const longestName = Math.max(...names.map((n) => n.length));

    term.print('Available commands:', 'system');
    for (const name of names) {
      // Pad every command name to the same width so descriptions line
      // up in a column, the way a real CLI's --help output does.
      const padded = name.padEnd(longestName + 2, ' ');
      term.print(`  ${padded}${commands[name].description}`);
    }
  },
};