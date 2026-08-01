/**
 * history.js
 * -----------------------------------------------------------------------
 * Command history recall (Up/Down arrow navigation), matching real shell
 * behavior: pressing Up walks backward through previously entered
 * commands; pressing Down walks forward; pressing Down past the most
 * recent entry restores whatever the visitor had been typing before
 * they started browsing history (the "draft"), rather than just
 * clearing the line.
 *
 * This module has no dependency on TerminalEmulator or DOM at all — it
 * is a plain data structure. Wiring it to actual arrow key presses
 * happens at the call site via TerminalEmulator's onHistoryUp/
 * onHistoryDown hooks (see the usage example at the bottom of this
 * file), keeping this class trivially unit-testable in isolation.
 * -----------------------------------------------------------------------
 */

const MAX_HISTORY = 200; // bounds memory growth over a long play session

export class CommandHistory {
  constructor() {
    /** @type {string[]} oldest-first list of previously submitted commands */
    this.entries = [];

    /**
     * Current position while browsing history. null means "not
     * currently browsing" (the normal state after a command is
     * submitted, or before any Up press). An index into `entries`
     * otherwise.
     * @type {number|null}
     */
    this.cursor = null;

    /**
     * Whatever was typed (but not yet submitted) at the moment Up was
     * first pressed. Restored when the visitor navigates Down past
     * the most recent history entry.
     * @type {string}
     */
    this.draft = '';
  }

  /**
   * Records a submitted command. Call this AFTER a line is submitted
   * (e.g. right after TerminalEmulator.readLine() resolves), not
   * during typing.
   *
   * Skips recording if the command is empty/whitespace, or identical
   * to the immediately preceding entry — matching common shell
   * behavior where repeatedly pressing Enter or re-running the same
   * command doesn't clutter history with duplicates.
   *
   * @param {string} command
   */
  push(command) {
    const trimmed = command.trim();
    if (trimmed.length === 0) return;

    const last = this.entries[this.entries.length - 1];
    if (trimmed === last) return;

    this.entries.push(trimmed);
    if (this.entries.length > MAX_HISTORY) {
      this.entries.shift();
    }

    this.cursor = null; // submitting always resets browsing position
  }

  /**
   * Navigates one step backward (toward older commands). Intended to
   * be passed as TerminalEmulator's onHistoryUp hook, which supplies
   * the input's current value as `currentValue` — needed here so the
   * very first Up press can stash an in-progress draft before
   * overwriting the input.
   *
   * @param {string} currentValue - the input's value at the moment
   *   Up was pressed.
   * @returns {string|undefined} the command to display, or undefined
   *   if there's no history to navigate to (leaves input unchanged).
   */
  up(currentValue) {
    if (this.entries.length === 0) return undefined;

    if (this.cursor === null) {
      this.draft = currentValue;
      this.cursor = this.entries.length - 1;
    } else if (this.cursor > 0) {
      this.cursor -= 1;
    }
    // else: already at the oldest entry — stay put, same as a real
    // shell not scrolling past the beginning of history.

    return this.entries[this.cursor];
  }

  /**
   * Navigates one step forward (toward newer commands), restoring the
   * pre-browsing draft once the visitor moves past the most recent
   * entry. Intended as TerminalEmulator's onHistoryDown hook.
   *
   * @returns {string|undefined} the command (or restored draft) to
   *   display, or undefined if Down is pressed while not currently
   *   browsing history (leaves input unchanged — there's nothing to
   *   navigate forward from).
   */
  down() {
    if (this.cursor === null) return undefined;

    if (this.cursor < this.entries.length - 1) {
      this.cursor += 1;
      return this.entries[this.cursor];
    }

    // Moved past the newest entry — hand back whatever was being
    // typed before history browsing started.
    this.cursor = null;
    return this.draft;
  }
}

/**
 * -----------------------------------------------------------------------
 * USAGE (for whichever file builds the real game loop / main.js):
 *
 *   const history = new CommandHistory();
 *
 *   while (true) {
 *     const input = await term.readLine({
 *       promptText: '> ',
 *       onHistoryUp: (currentValue) => history.up(currentValue),
 *       onHistoryDown: () => history.down(),
 *     });
 *     history.push(input);
 *     // ...dispatch input to CommandParser + command handlers...
 *   }
 * -----------------------------------------------------------------------
 */