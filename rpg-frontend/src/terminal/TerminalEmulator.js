/**
 * TerminalEmulator.js
 * -----------------------------------------------------------------------
 * A DOM-based terminal emulator: renders a scrollback of styled lines and
 * exposes an async readLine() for capturing user input one line at a
 * time. This module knows NOTHING about the game, commands, or auth —
 * it's pure presentation + input capture, the same separation of
 * concerns as the portfolio site's easter-egg modules (each does one
 * job, orchestration lives elsewhere).
 *
 * DESIGN NOTE — real <input>, not a hand-rolled fake cursor:
 * The input row uses an actual <input> element (styled to be invisible
 * as an "input box" and blend into the terminal line) rather than
 * capturing raw keydown events and drawing a fake cursor manually. This
 * is a deliberate choice for production quality: a real <input> gets
 * correct cursor movement, text selection, IME composition (for non-
 * Latin input), mobile on-screen keyboards, and password masking for
 * free from the browser — all of which a hand-rolled keydown-based
 * cursor would have to reimplement, imperfectly, from scratch.
 *
 * SECURITY NOTE — all printed text is inserted via textContent, never
 * innerHTML. This module has no way to render arbitrary HTML, which
 * matters once game content (eventually sourced from backend responses)
 * flows through print() — there is no path from "text the server sent"
 * to "markup the browser executes."
 * -----------------------------------------------------------------------
 */

// Line type -> CSS class, applied to each printed line's container.
// Kept as a small lookup rather than letting callers pass arbitrary
// class strings, so terminal.css only ever needs to style a known,
// closed set of line types.
const LINE_CLASSES = {
  output: 'term-line-output',
  error: 'term-line-error',
  prompt: 'term-line-prompt', // echoed input lines, e.g. "> look"
  system: 'term-line-system', // boot messages, connection status, etc.
};

export class TerminalEmulator {
  /**
   * @param {HTMLElement} container - the element this terminal renders
   *   into. Must be empty; this class takes full ownership of its
   *   contents.
   */
  constructor(container) {
    if (!container) {
      throw new Error('TerminalEmulator: container element is required');
    }
    this.container = container;
    this.container.classList.add('term-root');

    this.scrollback = document.createElement('div');
    this.scrollback.className = 'term-scrollback';
    this.container.appendChild(this.scrollback);

    this.inputRow = document.createElement('div');
    this.inputRow.className = 'term-input-row';

    this.promptEl = document.createElement('span');
    this.promptEl.className = 'term-prompt-symbol';

    this.inputEl = document.createElement('input');
    this.inputEl.className = 'term-input';
    this.inputEl.type = 'text';
    this.inputEl.autocomplete = 'off';
    this.inputEl.autocapitalize = 'off';
    this.inputEl.spellcheck = false;

    this.inputRow.appendChild(this.promptEl);
    this.inputRow.appendChild(this.inputEl);
    this.container.appendChild(this.inputRow);

    // Clicking anywhere in the terminal focuses the (possibly
    // visually hidden) input — this is what makes the whole panel
    // "feel" like a real terminal rather than requiring a precise
    // click on a thin input box.
    this.container.addEventListener('click', () => this.focus());

    // Set by readLine() while a read is in progress; readLine()
    // enforces only one read at a time (see the guard there) so this
    // is never ambiguous about which call it belongs to.
    this._pendingResolve = null;

    this.inputEl.addEventListener('keydown', (e) => this._handleKeydown(e));
  }

  /**
   * Prints a single line to the scrollback and auto-scrolls to keep it
   * in view. Text is inserted via textContent — see the SECURITY NOTE
   * at the top of this file for why that's non-negotiable here.
   *
   * @param {string} text
   * @param {'output'|'error'|'prompt'|'system'} [type='output']
   */
  print(text, type = 'output') {
    const line = document.createElement('div');
    line.className = `term-line ${LINE_CLASSES[type] || LINE_CLASSES.output}`;
    line.textContent = text;
    this.scrollback.appendChild(line);
    this._scrollToBottom();
  }

  /**
   * Prints multiple lines in one call — a small convenience so callers
   * don't have to loop over print() themselves for multi-line output
   * (e.g. a "help" command listing several commands).
   * @param {string[]} lines
   * @param {'output'|'error'|'prompt'|'system'} [type='output']
   */
  printLines(lines, type = 'output') {
    for (const line of lines) {
      this.print(line, type);
    }
  }

  /**
   * Displays a prompt and waits for the visitor to type a line and
   * press Enter. Resolves with the entered text (Enter's default form-
   * submission behavior is irrelevant here since there's no <form> —
   * this is driven entirely by a keydown listener on the input).
   *
   * @param {Object} [opts]
   * @param {string} [opts.promptText='> '] - shown before the input,
   *   e.g. "username: " or "> " for a game command prompt.
   * @param {boolean} [opts.secret=false] - if true, the input renders
   *   as a password field (native browser masking) and the line
   *   echoed to scrollback on submit is a fixed mask rather than the
   *   actual text — see _echoSubmittedLine for why a FIXED-length
   *   mask, not dots matching the real password length.
   * @param {(currentValue: string) => (string|undefined)} [opts.onHistoryUp] -
   *   called when ArrowUp is pressed, passed the input's current
   *   value (so history.js can stash it as an in-progress draft
   *   before navigating backward). Return a string to replace the
   *   input's value, or undefined to leave it unchanged. This module
   *   has no concept of "history" itself — it only forwards the key
   *   event and applies whatever the caller decides. Ignored if
   *   omitted (plain ArrowUp does nothing, same as before this hook
   *   existed).
   * @param {() => (string|undefined)} [opts.onHistoryDown] - same
   *   idea, for ArrowDown.
   * @returns {Promise<string>} the entered text, trimmed of leading/
   *   trailing whitespace.
   */
  readLine({ promptText = '> ', secret = false, onHistoryUp, onHistoryDown } = {}) {
    if (this._pendingResolve) {
      // Enforced one-at-a-time: readLine is meant to be awaited by
      // its caller before requesting another line. If this ever
      // fires, it's a bug in the calling code (e.g. two commands
      // both trying to read input concurrently), not a state this
      // module should try to silently queue or merge.
      return Promise.reject(
        new Error('TerminalEmulator: readLine() called while another read is already pending')
      );
    }

    this.promptEl.textContent = promptText;
    this.inputEl.type = secret ? 'password' : 'text';
    this.inputEl.value = '';
    this._currentSecret = secret;
    this._currentPromptText = promptText;
    this._currentOnHistoryUp = onHistoryUp;
    this._currentOnHistoryDown = onHistoryDown;

    this.setEnabled(true);
    this.focus();

    return new Promise((resolve) => {
      this._pendingResolve = resolve;
    });
  }

  /**
   * Enables or disables the input row. Used by callers to lock input
   * while an async operation (e.g. a login network request) is in
   * flight, so a visitor can't submit a second line before the first
   * has been handled.
   * @param {boolean} enabled
   */
  setEnabled(enabled) {
    this.inputEl.disabled = !enabled;
    this.inputRow.classList.toggle('term-input-row-disabled', !enabled);
  }

  /** Focuses the input element, if it isn't disabled. */
  focus() {
    if (!this.inputEl.disabled) {
      this.inputEl.focus();
    }
  }

  /** Clears all scrollback content. Does not affect the input row. */
  clear() {
    this.scrollback.innerHTML = '';
  }

  // ---------------------------------------------------------------
  // Internal
  // ---------------------------------------------------------------

  _handleKeydown(e) {
    if (e.key === 'ArrowUp') {
      if (!this._currentOnHistoryUp) return;
      e.preventDefault();
      const replacement = this._currentOnHistoryUp(this.inputEl.value);
      if (replacement !== undefined) this.inputEl.value = replacement;
      return;
    }

    if (e.key === 'ArrowDown') {
      if (!this._currentOnHistoryDown) return;
      e.preventDefault();
      const replacement = this._currentOnHistoryDown();
      if (replacement !== undefined) this.inputEl.value = replacement;
      return;
    }

    if (e.key !== 'Enter') return;
    if (!this._pendingResolve) return; // no read in progress — ignore

    const value = this.inputEl.value.trim();
    this._echoSubmittedLine(value);

    const resolve = this._pendingResolve;
    this._pendingResolve = null;
    this.inputEl.value = '';

    resolve(value);
  }

  /**
   * Echoes the just-submitted line into the scrollback as a
   * "term-line-prompt" entry, so the terminal reads as a continuous
   * transcript (prompt + what was typed + the response that follows),
   * exactly like a real shell session.
   *
   * For secret input, this deliberately echoes a FIXED-length mask
   * ("••••••••") rather than dots matching the real password's
   * length. Echoing length-matched dots would leak the password's
   * length into the permanent, scrollable transcript — a fixed mask
   * avoids that while still visually confirming "a password was
   * submitted here."
   */
  _echoSubmittedLine(value) {
    const displayValue = this._currentSecret ? '••••••••' : value;
    this.print(`${this._currentPromptText}${displayValue}`, 'prompt');
  }

  _scrollToBottom() {
    this.scrollback.scrollTop = this.scrollback.scrollHeight;
  }
}