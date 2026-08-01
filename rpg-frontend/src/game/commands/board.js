/**
 * board.js
 * -----------------------------------------------------------------------
 * The tavern's community board: `board`, `note "<message>"`, and
 * `pin "<message>"`.
 *
 * "Cycled through time-based" is implemented as advancing one note per
 * `board` call (newest-first order, wrapping back to the newest once
 * you've cycled past the oldest) rather than a real background timer —
 * TerminalEmulator.js's readLine() blocks the command loop on user
 * input (see main.js's runCommandLoop), so there's no clean way for
 * this frontend to keep printing on an interval WHILE also waiting for
 * the next line of input the way a true auto-rotating display would
 * need to. `board all` is provided alongside for anyone who wants the
 * full list at once instead of stepping through it call-by-call.
 * Pinned notes are never part of that cycle — see below, they always
 * print in full, every time.
 *
 * boardCycleIndex is module-level (not on GameState) deliberately —
 * it's pure UI/display position, not character save data, so it has
 * no business living anywhere near _applyGameState's mirror of the
 * backend.
 *
 * PIN_NOTE_GOLD_COST is display-only content, same "must be kept in
 * sync by hand" tradeoff tavern.js's SHOP_ITEMS already accepts — the
 * real, enforced cost is handlers/board.go's pinnedNoteGoldCost.
 * -----------------------------------------------------------------------
 */

const PIN_NOTE_GOLD_COST = 4;

let boardCycleIndex = 0;

function formatStamp(createdAt) {
  const when = new Date(createdAt);
  return Number.isNaN(when.getTime()) ? createdAt : when.toLocaleString();
}

function printPinned(term, pinned) {
  if (pinned.length === 0) return;
  term.print('Pinned (permanent):', 'system');
  for (const note of pinned) {
    term.print(`  \u2605 ${note.username} — ${formatStamp(note.createdAt)}`);
    term.print(`    "${note.message}"`);
  }
}

export const boardCommand = {
  name: 'board',
  description: 'Read the tavern community board: board [all]',

  async execute({ term, args, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <name> <class> <difficulty>" to begin.', 'error');
      return;
    }
    if (!gameState.atTavern) {
      term.print('You need to be in the tavern to read the board. Type "tavern" to check.', 'error');
      return;
    }

    let board;
    try {
      board = await gameState.getBoardNotes();
    } catch (err) {
      term.print(err.message || 'Could not read the board.', 'error');
      return;
    }

    const { pinned, notes } = board;

    if (pinned.length === 0 && notes.length === 0) {
      term.print('The community board is empty. Be the first to leave a note.', 'system');
      return;
    }

    printPinned(term, pinned);

    if (notes.length === 0) {
      return;
    }

    if (args[0] && args[0].toLowerCase() === 'all') {
      if (pinned.length > 0) term.print('', 'system');
      term.print('All notes:', 'system');
      notes.forEach((note, i) => {
        term.print(`[${i + 1}/${notes.length}] ${note.username} — ${formatStamp(note.createdAt)}`);
        term.print(`  "${note.message}"`);
      });
      return;
    }

    // Cycle one note per call, wrapping around. Reset to the top if a
    // prior session's index no longer fits (e.g. the board shrank,
    // not that it ever will, but defensive is cheap here).
    if (boardCycleIndex >= notes.length) boardCycleIndex = 0;

    const note = notes[boardCycleIndex];
    if (pinned.length > 0) term.print('', 'system');
    term.print(`[${boardCycleIndex + 1}/${notes.length}] ${note.username} — ${formatStamp(note.createdAt)}`);
    term.print(`  "${note.message}"`);
    term.print('Type "board" again for the next note, or "board all" to see every note.', 'system');

    boardCycleIndex = (boardCycleIndex + 1) % notes.length;
  },
};

export const noteCommand = {
  name: 'note',
  description: 'Leave a note on the tavern community board: note "<message>"',

  async execute({ term, args, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <name> <class> <difficulty>" to begin.', 'error');
      return;
    }
    if (!gameState.atTavern) {
      term.print('You need to be in the tavern to leave a note. Type "tavern" to check.', 'error');
      return;
    }

    const message = args.join(' ').trim();
    if (!message) {
      term.print('Usage: note "<message>"', 'error');
      return;
    }

    try {
      await gameState.postBoardNote(message);
    } catch (err) {
      term.print(err.message || 'Could not post that note.', 'error');
      return;
    }

    term.print('Your note is added to the community board.', 'system');
  },
};

export const pinCommand = {
  name: 'pin',
  description: `Leave a permanent note on the board for ${PIN_NOTE_GOLD_COST} gold: pin "<message>"`,

  async execute({ term, args, gameState }) {
    if (!gameState.hasCharacter) {
      term.print('You have no character yet. Type "create <name> <class> <difficulty>" to begin.', 'error');
      return;
    }
    if (!gameState.atTavern) {
      term.print('You need to be in the tavern to pin a note. Type "tavern" to check.', 'error');
      return;
    }

    const message = args.join(' ').trim();
    if (!message) {
      term.print(`Usage: pin "<message>" (costs ${PIN_NOTE_GOLD_COST} gold)`, 'error');
      return;
    }

    // Refresh first so gold reflects anything earned/spent very
    // recently — same reason tavern.js's execute() reloads before
    // acting rather than trusting whatever gameState already has in
    // memory.
    try {
      await gameState.loadGame();
    } catch (err) {
      term.print(err.message || 'Could not reach the tavern.', 'error');
      return;
    }

    if (gameState.gold < PIN_NOTE_GOLD_COST) {
      term.print(`You need ${PIN_NOTE_GOLD_COST} gold to pin a note (you have ${gameState.gold}).`, 'error');
      return;
    }

    try {
      await gameState.postBoardNote(message, true);
    } catch (err) {
      term.print(err.message || 'Could not pin that note.', 'error');
      return;
    }

    term.print(`Your note is pinned to the community board — permanently, for ${PIN_NOTE_GOLD_COST} gold.`, 'system');
    term.print(`Gold ${gameState.gold}`, 'system');
  },
};
