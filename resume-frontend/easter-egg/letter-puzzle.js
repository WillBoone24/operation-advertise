/**
 * letter-puzzle.js
 * -----------------------------------------------------------------------
 * Stage 2: the "THANK YOU" letter hunt — found IN ORDER.
 *
 * The 8 letters of "THANK YOU" are single characters already woven
 * into index.html's real copy — one letter inside a project
 * description, one inside the footer heading, one inside the stack
 * list, etc. — each marked with `data-ee-letter="X"` on a <span>.
 *
 * Only ONE letter span is ever interactive at a time: the next one due
 * in T-H-A-N-K-Y-O-U order. Every other letter (already found, or not
 * yet due) is inert plain text — clicking ahead does nothing, because
 * nothing but the current target has a listener attached. This means
 * the DOM order the letters happen to appear in on the page is
 * irrelevant; only PHRASE's order matters. A visitor who stumbles onto
 * the 5th letter before the 1st sees nothing special about it yet.
 *
 * Single responsibility: activate exactly the current pre-marked
 * letter span, track collection progress, render a small persistent
 * HUD with a hangman-style blanks display, and report completion via
 * callback. Like the other stage modules, it does not call
 * state.advanceStage() for the *next* stage transition — it only
 * reports "I'm done." (It DOES read state.js to self-gate, same
 * pattern as bouncing-logo.js and registration.js.)
 *
 * IMPORTANT SCOPE NOTE: only elements bearing `data-ee-letter` are ever
 * touched, and only one of those at any given moment. This module has
 * no code path that touches any other text on the page.
 * -----------------------------------------------------------------------
 */

import {
  STAGES,
  canActivate,
  getStage,
  getCollectedLetters,
  addCollectedLetter,
  clearCollectedLetters,
} from './state.js';

// The collectible phrase, in the exact order letters must be found.
// All 8 letters are unique (T, H, A, N, K, Y, O, U — no repeats), each
// with exactly one matching `data-ee-letter` span somewhere in
// index.html. The space and "!!" are fixed/decorative in the blanks
// display — they were never something to hunt for.
const PHRASE = 'THANK YOU';
const SUFFIX = '!!';
const REQUIRED_LETTERS = Array.from(new Set(PHRASE.replace(/[^A-Z]/g, '').split('')));

const LETTER_SELECTOR = '[data-ee-letter]';

/**
 * Activates the in-page, in-order letter hunt.
 *
 * @param {Object} opts
 * @param {() => void} opts.onComplete - called once, when every letter
 *   in PHRASE has been collected in order.
 * @returns {{ destroy: () => void } | null}
 */
export function mountLetterPuzzle({ onComplete }) {
  if (!canActivate(STAGES.LOGO_CLICKED)) {
    return null;
  }
  if (getStage() !== STAGES.LOGO_CLICKED) {
    return null;
  }

  const targets = Array.from(document.querySelectorAll(LETTER_SELECTOR));
  const letterMap = new Map(); // 'T' -> <span> etc.
  targets.forEach((el) => {
    const letter = el.dataset.eeLetter;
    if (REQUIRED_LETTERS.includes(letter)) {
      letterMap.set(letter, el);
    }
  });

  const missing = REQUIRED_LETTERS.filter((l) => !letterMap.has(l));
  if (missing.length > 0) {
    console.error(
      `[easter-egg] letter-puzzle: no [data-ee-letter] span found in the ` +
      `page for: ${missing.join(', ')}. The hunt cannot be completed until ` +
      `index.html has one span per required letter.`
    );
  }

  // Resume progress across a page reload mid-hunt. Progress is defined
  // purely by count — "the first N letters of REQUIRED_LETTERS are
  // done" — since order is enforced at collection time, not by which
  // letters happen to be in the stored array.
  let progress = Math.min(getCollectedLetters().length, REQUIRED_LETTERS.length);

  // Mark everything already found (from a prior page load) as
  // permanently collected, with no listeners.
  for (let i = 0; i < progress; i++) {
    const el = letterMap.get(REQUIRED_LETTERS[i]);
    if (el) el.classList.add('ee-hidden-letter-collected');
  }

  // ---- HUD: small persistent progress indicator -----------------------
  const hud = document.createElement('div');
  hud.className = 'ee-hud';

  const hudLabel = document.createElement('div');
  hudLabel.className = 'ee-hud-label';
  hudLabel.textContent = 'discovery / stage 2';
  hud.appendChild(hudLabel);

  const hudBlanks = document.createElement('div');
  hudBlanks.className = 'ee-hud-blanks';
  hud.appendChild(hudBlanks);

  document.body.appendChild(hud);
  requestAnimationFrame(() => hud.classList.add('ee-visible'));

  function renderBlanks() {
    let out = '';
    let letterIndex = 0;
    for (const ch of PHRASE) {
      if (ch === ' ') {
        out += '   ';
      } else {
        out += (letterIndex < progress ? ch : '_') + ' ';
        letterIndex++;
      }
    }
    hudBlanks.textContent = out.trim() + SUFFIX;
  }

  // ---- Only the current letter in sequence is ever wired up ----------
  let activeCleanup = null;

  function activateCurrent() {
    if (progress >= REQUIRED_LETTERS.length) return; // hunt already done

    const letter = REQUIRED_LETTERS[progress];
    const el = letterMap.get(letter);
    if (!el) return; // already warned above; nothing to wire up

    el.classList.add('ee-hidden-letter-active');
    el.setAttribute('role', 'button');
    el.setAttribute('tabindex', '0');
    el.setAttribute('aria-label', `Collect hidden letter ${letter}`);

    function collect() {
      el.classList.remove('ee-hidden-letter-active');
      el.classList.add('ee-hidden-letter-collected');
      el.removeAttribute('role');
      el.removeAttribute('tabindex');
      el.removeAttribute('aria-label');
      if (activeCleanup) {
        activeCleanup();
        activeCleanup = null;
      }

      progress += 1;
      addCollectedLetter(letter);
      renderBlanks();

      if (progress >= REQUIRED_LETTERS.length) {
        // Small delay so the visitor sees their final click register
        // before the HUD disappears — an instant cut feels like a
        // glitch rather than a completion.
        setTimeout(() => {
          clearCollectedLetters(); // this session's hunt state is now irrelevant
          destroy();
          onComplete();
        }, 600);
      } else {
        activateCurrent(); // wire up the next letter in order
      }
    }

    function handleClick(e) {
      // These letters live inside ordinary paragraph/heading text, not
      // links, so there's no default navigation to prevent — but stop
      // propagation defensively in case a future edit ever nests one
      // inside a clickable ancestor.
      e.stopPropagation();
      collect();
    }
    function handleKeydown(e) {
      if (e.key === 'Enter' || e.key === ' ') {
        e.preventDefault();
        collect();
      }
    }

    el.addEventListener('click', handleClick);
    el.addEventListener('keydown', handleKeydown);
    activeCleanup = () => {
      el.removeEventListener('click', handleClick);
      el.removeEventListener('keydown', handleKeydown);
    };
  }

  renderBlanks();
  activateCurrent();

  let destroyed = false;
  function destroy() {
    if (destroyed) return;
    destroyed = true;
    // Only the HUD and the one currently-wired listener are torn down.
    // The letter spans themselves are permanent page content (not
    // overlay elements) and are intentionally left in the DOM.
    if (activeCleanup) activeCleanup();
    hud.classList.remove('ee-visible');
    setTimeout(() => hud.remove(), 260);
  }

  return { destroy };
}