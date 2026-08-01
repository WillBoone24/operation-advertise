/**
 * login.js
 * -----------------------------------------------------------------------
 * Renders a terminal-style login prompt and loops until authentication
 * succeeds. This is a LOGIN-ONLY flow — per the earlier project
 * decision, account creation stays exclusive to the portfolio site's
 * easter egg. There is no registration path here, and no convenience
 * method in api/client.js to fall back on even if someone tried to add
 * one carelessly.
 *
 * Depends on TerminalEmulator (for readLine) and api/client.js (for the
 * actual network call) — nothing else. Does not know about GameState or
 * game commands; it hands back the login result and its caller (the
 * future main.js) decides what happens next.
 * -----------------------------------------------------------------------
 */

import { login as apiLogin, ApiError, NetworkError } from '../api/client.js';

/**
 * Runs the login flow against the given terminal, looping on failure
 * until credentials are accepted. There is no "give up" exit — for a
 * login-gated experience, there is nothing useful to fall through to
 * without a session, so this deliberately keeps prompting rather than
 * stranding the visitor in a half-authenticated state.
 *
 * @param {import('../terminal/TerminalEmulator.js').TerminalEmulator} term
 * @returns {Promise<{ token: string, user_id: string }>} resolves once
 *   login succeeds (api/client.js has already persisted the session by
 *   the time this resolves).
 */
export async function runLogin(term) {
  term.print('Authentication required.', 'system');
  term.print('', 'system');

  // eslint-disable-next-line no-constant-condition
  while (true) {
    const username = await term.readLine({ promptText: 'username: ' });
    if (username.length === 0) {
      term.print('Username cannot be empty.', 'error');
      continue;
    }

    const password = await term.readLine({ promptText: 'password: ', secret: true });
    if (password.length === 0) {
      term.print('Password cannot be empty.', 'error');
      continue;
    }

    // Locking input during the network round-trip prevents a second
    // submission racing the first — the same concern the portfolio's
    // registration.js handles by re-rendering into a "Registering…"
    // state, expressed here as TerminalEmulator's setEnabled(false).
    term.setEnabled(false);
    term.print('Authenticating...', 'system');

    try {
      const result = await apiLogin(username, password);
      term.print(`Welcome back, ${username}.`, 'system');
      return result;
    } catch (err) {
      term.print(describeLoginError(err), 'error');
      // Falls through to the top of the loop and prompts again —
      // setEnabled(true) happens in `finally` below either way.
    } finally {
      term.setEnabled(true);
    }
  }
}

/**
 * Maps a thrown error from api/client.js into a message appropriate to
 * show the visitor. Kept as its own function (rather than inlined in
 * the catch block above) so the error-message policy is in one place
 * and easy to adjust independently of the retry-loop control flow.
 *
 * @param {unknown} err
 * @returns {string}
 */
function describeLoginError(err) {
  if (err instanceof ApiError) {
    // The backend's handlers.Login deliberately returns an identical,
    // generic message for both "unknown username" and "wrong
    // password" (see the backend's anti-enumeration design) — we
    // display that message as-is rather than trying to add our own
    // interpretation on top of it.
    return err.message;
  }
  if (err instanceof NetworkError) {
    return err.message;
  }
  // Anything else (a genuine bug, an unexpected response shape) is
  // logged for debugging but shown to the visitor as a generic
  // message — same reasoning as registration.js not leaking internal
  // error detail to the UI.
  console.error('[rpg] unexpected login error:', err);
  return 'Something went wrong. Please try again.';
}