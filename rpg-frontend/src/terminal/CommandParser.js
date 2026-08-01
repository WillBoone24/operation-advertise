/**
 * CommandParser.js
 * -----------------------------------------------------------------------
 * Turns a raw line of typed input into a structured { command, args, raw }
 * shape. Knows nothing about which commands exist or what they do —
 * that dispatch logic belongs to whatever owns the game loop (a future
 * main.js / command registry). This file's only job is tokenization.
 * -----------------------------------------------------------------------
 */

/**
 * @typedef {Object} ParsedCommand
 * @property {string} command - lowercased command name, e.g. "look"
 * @property {string[]} args - remaining tokens, quote-aware (see below)
 * @property {string} raw - the original, untrimmed input line
 */

/**
 * Parses a raw input line into a command + arguments.
 *
 * Supports double-quoted arguments so a single argument can contain
 * spaces, e.g.:
 *   say "hello there"   -> { command: 'say', args: ['hello there'] }
 * A future "say" command handling player chat is the obvious case this
 * exists for — without quote support, chat messages could never contain
 * spaces, which would be an unacceptably broken player experience for a
 * text-based game.
 *
 * @param {string} input - raw line from TerminalEmulator.readLine()
 * @returns {ParsedCommand|null} null if the input is empty/whitespace-only
 */
export function parseCommand(input) {
  const raw = input;
  const trimmed = input.trim();

  if (trimmed.length === 0) {
    return null;
  }

  const tokens = tokenize(trimmed);
  const [command, ...args] = tokens;

  return {
    command: command.toLowerCase(),
    args,
    raw,
  };
}

/**
 * Splits a string into tokens on whitespace, EXCEPT inside double
 * quotes, where whitespace is preserved as part of a single token.
 * The surrounding quote characters themselves are stripped from the
 * resulting token.
 *
 * Implemented as an explicit character-by-character scan rather than a
 * regex — quote-aware tokenization with escaping edge cases (e.g. what
 * should `say "she said "hi""` do) is exactly the kind of thing regex
 * solutions get subtly wrong. A small state machine is easier to read,
 * easier to verify by inspection, and easier to extend later (e.g. if
 * single-quote or backslash-escape support is ever needed).
 *
 * @param {string} str
 * @returns {string[]}
 */
function tokenize(str) {
  const tokens = [];
  let current = '';
  let inQuotes = false;

  for (let i = 0; i < str.length; i++) {
    const ch = str[i];

    if (ch === '"') {
      inQuotes = !inQuotes;
      continue; // quote character itself is never part of the token
    }

    if (ch === ' ' && !inQuotes) {
      if (current.length > 0) {
        tokens.push(current);
        current = '';
      }
      continue;
    }

    current += ch;
  }

  // An unterminated quote (e.g. `say "hello) still yields whatever was
  // accumulated rather than silently dropping it — malformed input
  // should degrade gracefully, not lose data the player typed.
  if (current.length > 0) {
    tokens.push(current);
  }

  return tokens;
}