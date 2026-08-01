/**
 * main.js
 * -----------------------------------------------------------------------
 * The RPG frontend's real entry point. This is the ONLY file that
 * imports every command module — same principle as the portfolio's
 * easter-egg main.js being the sole file that imports all three stage
 * modules. Individual command files never import each other or this
 * file, which keeps them independently reasoned-about and avoids
 * circular imports (see help.js's doc comment for the specific case
 * this prevents).
 *
 * Boot sequence:
 *   1. Mount TerminalEmulator.
 *   2. If a stored token exists, try GET /api/me to confirm it's still
 *      valid. If that fails (expired/invalid), fall through to login
 *      exactly as if no token had existed — api/client.js already
 *      clears a dead token on any 401, so this doesn't need to
 *      duplicate that cleanup.
 *   3. If no valid session, run the login loop until it succeeds.
 *   4. Load GameState (if step 2 didn't already do it).
 *   5. Enter the command loop: read a line, parse it, dispatch to the
 *      matching registered command, repeat forever.
 * -----------------------------------------------------------------------
 */

import { TerminalEmulator } from './terminal/TerminalEmulator.js';
import { parseCommand } from './terminal/CommandParser.js';
import { CommandHistory } from './terminal/history.js';
import { isAuthenticated } from './api/client.js';
import { runLogin } from './auth/login.js';
import { GameState } from './game/GameState.js';

import { whoamiCommand } from './game/commands/whoami.js';
import { helpCommand } from './game/commands/help.js';
import { statusCommand } from './game/commands/status.js';
import { exitCommand } from './game/commands/exit.js';
import { createCommand } from './game/commands/create.js';
import { lookCommand } from './game/commands/look.js';
import { attackCommand } from './game/commands/attack.js';
import { equipCommand } from './game/commands/equip.js';
import { useCommand } from './game/commands/use.js';
import { castCommand } from './game/commands/cast.js';
import { abilityCommand } from './game/commands/ability.js';
import { inventoryCommand } from './game/commands/inventory.js';
import { inspectCommand } from './game/commands/inspect.js';
import { descendCommand } from './game/commands/descend.js';
import { tavernCommand } from './game/commands/tavern.js';
import { hallCommand } from './game/commands/hall.js';
import { boardCommand, noteCommand, pinCommand } from './game/commands/board.js';

// The full command registry, keyed by name. This is the ONE place a
// new command needs to be added going forward — every command file
// itself stays ignorant of this list (see help.js's doc comment on
// why, and the shared ctx-passing pattern that keeps it that way).
const COMMANDS = buildRegistry([
  whoamiCommand,
  helpCommand,
  statusCommand,
  exitCommand,
  createCommand,
  lookCommand,
  attackCommand,
  equipCommand,
  useCommand,
  castCommand,
  abilityCommand,
  inventoryCommand,
  inspectCommand,
  descendCommand,
  tavernCommand,
  hallCommand,
  boardCommand,
  noteCommand,
  pinCommand,
]);

function buildRegistry(commandList) {
  const registry = {};
  for (const command of commandList) {
    if (registry[command.name]) {
      // A duplicate command name is a bug in this file's own list,
      // not something a player could ever trigger — failing loudly
      // at boot is far preferable to silently letting the second
      // registration shadow the first.
      throw new Error(`main: duplicate command name registered: "${command.name}"`);
    }
    registry[command.name] = command;
  }
  return registry;
}

async function boot() {
  const term = new TerminalEmulator(document.getElementById('term-root'));
  term.print('will@uark — rpg terminal', 'system');
  term.print('', 'system');

  const gameState = new GameState();

  // Step 2: an existing token doesn't guarantee it's still valid
  // (expired, or invalidated server-side) — the only way to know for
  // certain is to actually ask the backend.
  let sessionReady = false;
  if (isAuthenticated()) {
    try {
      await gameState.load();
      // loadGame() treats "no character yet" as a normal outcome, not
      // an error (see GameState.js) — it's safe to call unconditionally
      // right after a successful profile load, whether or not a
      // character exists.
      await gameState.loadGame();
      sessionReady = true;
    } catch {
      // api/client.js's request() already cleared the dead token on
      // the 401 that caused this — nothing more to clean up here.
      // Falling through to the login branch below is the correct
      // recovery, not an error state worth surfacing differently.
    }
  }

  if (!sessionReady) {
    await runLogin(term);
    await gameState.load();
    await gameState.loadGame();
  }

  term.print('', 'system');
  term.print(`Logged in as ${gameState.username}. Type "help" to get started.`, 'system');
  if (!gameState.hasCharacter) {
    term.print('You have no character yet. Type "create <name> <class> <difficulty>" to begin.', 'system');
  }
  term.print('', 'system');

  await runCommandLoop(term, gameState);
}

/**
 * The main interactive loop: read a line, parse it, dispatch to a
 * registered command, repeat. Runs forever — there is currently no
 * "quit" command, matching the fact that there's nowhere else in this
 * frontend to go (no other page, no other view). Adding one later is a
 * one-line registry addition, not a structural change.
 *
 * @param {TerminalEmulator} term
 * @param {GameState} gameState
 */
async function runCommandLoop(term, gameState) {
  const history = new CommandHistory();

  // eslint-disable-next-line no-constant-condition
  while (true) {
    const line = await term.readLine({
      promptText: '> ',
      onHistoryUp: (currentValue) => history.up(currentValue),
      onHistoryDown: () => history.down(),
    });

    history.push(line);

    const parsed = parseCommand(line);
    if (parsed === null) continue; // empty input — silently re-prompt, matching real shells

    const command = COMMANDS[parsed.command];
    if (!command) {
      term.print(`Unknown command: "${parsed.command}". Type "help" for a list of commands.`, 'error');
      continue;
    }

    try {
      await command.execute({ term, args: parsed.args, gameState, commands: COMMANDS });
    } catch (err) {
      // A command throwing is a bug in that command, not something
      // that should crash the whole session — log it for debugging
      // and keep the loop alive so one broken command doesn't take
      // down the entire terminal.
      console.error(`[rpg] command "${parsed.command}" threw:`, err);
      term.print('That command hit an unexpected error.', 'error');
    }
  }
}

boot();