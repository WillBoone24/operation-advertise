/**
 * registration.js
 * -----------------------------------------------------------------------
 * Stage 3: registration form, wired to the backend's POST /api/register.
 * Stage 4: "Bonus Game Unlocked" confirmation + link, shown immediately
 * on successful registration.
 *
 * These two stages are combined in one file rather than split into a
 * separate BonusGameLink module: they are not independently activatable
 * (stage 4 is not a state a visitor can be "at" without having just
 * completed stage 3 in the same moment) and share one continuous
 * animated panel transition. If the bonus game link ever needs to be
 * shown independently of a fresh registration (e.g. a "you're already
 * registered, here's your link again" path), that's a sign to split it
 * out — not needed for the current one-time-discovery flow.
 *
 * Per the spec's division of responsibilities:
 *   Frontend (this file): form display, UI transitions, success/failure
 *     states.
 *   Backend (already built): registration, credential validation, token
 *     generation, access control.
 * This file never validates credentials itself beyond basic client-side
 * shape checks (matching the backend's own minimums, for fast feedback)
 * — the backend's response is always the actual authority.
 * -----------------------------------------------------------------------
 */

import { STAGES, canActivate, getStage, advanceStage, markDiscoveryComplete } from './state.js';

// ---------------------------------------------------------------------
// Configuration — read from the generated config rather than
// hardcoded. See ./config.js's doc comment for why (WSL2 localhost
// forwarding is broken in this dev environment; scripts/dev-up.sh
// refreshes this file with the current WSL IP once per session).
// ---------------------------------------------------------------------
import { API_BASE_URL, BONUS_GAME_URL } from './config.js';

// Mirrors the backend's own minimums (see auth.HashPassword's
// minPasswordLength and handlers.Register's username bounds) purely
// for fast client-side feedback. The backend re-validates everything
// server-side regardless — these are UX conveniences, not a security
// boundary.
const MIN_USERNAME_LENGTH = 3;
const MAX_USERNAME_LENGTH = 32;
const MIN_PASSWORD_LENGTH = 8;

/**
 * Mounts the registration panel.
 *
 * @param {Object} opts
 * @param {() => void} opts.onComplete - called once, after the visitor
 *   has seen the "Bonus Game Unlocked" state and the whole sequence is
 *   considered finished (used by main.js to know discovery is fully
 *   done, though markDiscoveryComplete() is also called directly here).
 * @returns {{ destroy: () => void } | null}
 */
export function mountRegistration({ onComplete }) {
  if (!canActivate(STAGES.PUZZLE_COMPLETE)) {
    return null;
  }
  if (getStage() === STAGES.REGISTERED) {
    // Already fully registered in a prior session on this browser —
    // isDiscoveryComplete() in main.js should already be preventing
    // this module from ever being reached, but this is a second,
    // cheap line of defense against re-showing the form.
    return null;
  }

  const overlay = document.createElement('div');
  overlay.className = 'ee-overlay';

  const panel = document.createElement('div');
  panel.className = 'ee-panel';
  overlay.appendChild(panel);
  document.body.appendChild(overlay);

  renderFormState(panel, { onComplete });

  requestAnimationFrame(() => overlay.classList.add('ee-visible'));

  let destroyed = false;
  function destroy() {
    if (destroyed) return;
    destroyed = true;
    overlay.remove();
  }

  return { destroy };
}

// ---------------------------------------------------------------------
// Internal rendering — kept as small functions that each rebuild the
// panel's inner content for a given UI state (form / submitting /
// error / success), rather than a single sprawling function with
// branching innerHTML. Each is easy to reason about independently.
// ---------------------------------------------------------------------

function renderFormState(panel, { onComplete, errorMessage = null }) {
  panel.innerHTML = '';

  const heading = document.createElement('div');
  heading.className = 'ee-panel-label';
  heading.textContent = 'discovery / stage 3';
  panel.appendChild(heading);

  const title = document.createElement('h3');
  title.className = 'ee-panel-title';
  title.textContent = 'Register for the bonus experience';
  panel.appendChild(title);

  const form = document.createElement('form');
  form.className = 'ee-form';
  form.noValidate = true; // we handle validation/messaging ourselves for consistent styling

  const usernameField = buildField('text', 'ee-username', 'Username');
  const passwordField = buildField('password', 'ee-password', 'Password');
  form.appendChild(usernameField.wrapper);
  form.appendChild(passwordField.wrapper);

  const errorEl = document.createElement('div');
  errorEl.className = 'ee-form-error';
  if (errorMessage) {
    errorEl.textContent = errorMessage;
    errorEl.classList.add('ee-visible-inline');
  }
  form.appendChild(errorEl);

  const submitBtn = document.createElement('button');
  submitBtn.type = 'submit';
  submitBtn.className = 'ee-btn ee-btn-primary';
  submitBtn.textContent = 'Register';
  form.appendChild(submitBtn);

  form.addEventListener('submit', (e) => {
    e.preventDefault();
    handleSubmit(panel, {
      username: usernameField.input.value.trim(),
      password: passwordField.input.value,
      onComplete,
    });
  });

  panel.appendChild(form);
}

function buildField(type, id, labelText) {
  const wrapper = document.createElement('div');
  wrapper.className = 'ee-field';

  const label = document.createElement('label');
  label.setAttribute('for', id);
  label.textContent = labelText;

  const input = document.createElement('input');
  input.type = type;
  input.id = id;
  input.autocomplete = type === 'password' ? 'new-password' : 'username';

  wrapper.appendChild(label);
  wrapper.appendChild(input);

  return { wrapper, input };
}

function renderSubmittingState(panel) {
  panel.innerHTML = '';

  const heading = document.createElement('div');
  heading.className = 'ee-panel-label';
  heading.textContent = 'discovery / stage 3';
  panel.appendChild(heading);

  const msg = document.createElement('div');
  msg.className = 'ee-panel-status';
  msg.textContent = 'Registering…';
  panel.appendChild(msg);
}

function renderSuccessState(panel, { onComplete }) {
  panel.innerHTML = '';

  const heading = document.createElement('div');
  heading.className = 'ee-panel-label';
  heading.textContent = 'discovery / stage 4';
  panel.appendChild(heading);

  const title = document.createElement('h3');
  title.className = 'ee-panel-title';
  title.textContent = 'Bonus Game Unlocked';
  panel.appendChild(title);

  const reminder = document.createElement('p');
  reminder.className = 'ee-panel-body';
  reminder.textContent =
    'The bonus experience is accessed through this website URL. ' +
    'Please save or bookmark this address, as the link will not be provided again.';
  panel.appendChild(reminder);

  const urlDisplay = document.createElement('p');
  urlDisplay.className = 'ee-panel-body ee-panel-url';
  urlDisplay.textContent = BONUS_GAME_URL;
  panel.appendChild(urlDisplay);

  const closeBtn = document.createElement('button');
  closeBtn.type = 'button';
  closeBtn.className = 'ee-btn ee-btn-secondary';
  closeBtn.textContent = 'Return to site';
  closeBtn.addEventListener('click', onComplete);
  panel.appendChild(closeBtn);
}

// ---------------------------------------------------------------------
// Submission handling
// ---------------------------------------------------------------------

function validateClientSide(username, password) {
  if (username.length < MIN_USERNAME_LENGTH || username.length > MAX_USERNAME_LENGTH) {
    return `Username must be between ${MIN_USERNAME_LENGTH} and ${MAX_USERNAME_LENGTH} characters.`;
  }
  if (password.length < MIN_PASSWORD_LENGTH) {
    return `Password must be at least ${MIN_PASSWORD_LENGTH} characters.`;
  }
  return null;
}

async function handleSubmit(panel, { username, password, onComplete }) {
  const clientError = validateClientSide(username, password);
  if (clientError) {
    renderFormState(panel, { onComplete, errorMessage: clientError });
    return;
  }

  renderSubmittingState(panel);

  let response;
  try {
    response = await fetch(`${API_BASE_URL}/api/register`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password }),
    });
  } catch (networkErr) {
    // fetch() itself throws on network failure (server down, CORS
    // rejection, offline) — distinct from the server responding with
    // an error status, which is handled below.
    console.error('[easter-egg] registration network error:', networkErr);
    renderFormState(panel, {
      onComplete,
      errorMessage: 'Could not reach the server. Check your connection and try again.',
    });
    return;
  }

  let body;
  try {
    body = await response.json();
  } catch {
    body = null;
  }

  if (!response.ok) {
    // Mirrors the backend's actual response shapes from handlers/auth.go:
    // 400 (validation), 409 (username taken), 500 (server error) all
    // return { error: "..." } — we surface that message directly since
    // the backend already writes user-appropriate text.
    const message = body?.error || `Registration failed (${response.status}).`;
    renderFormState(panel, { onComplete, errorMessage: message });
    return;
  }

  // Success: backend returns { token, user_id } per handlers.authResponse.
  const { token, user_id: userId } = body;

  if (token && userId) {
    // Stored for potential future use (e.g. the bonus game frontend
    // eventually needing to hand off this session). NOTE: this is a
    // deliberately deferred design question, not a finished handoff
    // mechanism — see the flag below.
    localStorage.setItem('ee_auth_token', token);
    localStorage.setItem('ee_user_id', userId);
  }

  advanceStage(STAGES.REGISTERED);
  markDiscoveryComplete();

  renderSuccessState(panel, { onComplete });
}