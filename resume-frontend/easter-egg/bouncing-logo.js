/**
 * bouncing-logo.js
 * -----------------------------------------------------------------------
 * Stage 1: the hidden, clickable "DVD screensaver" logo that kicks off
 * the easter egg sequence.
 *
 * Single responsibility: render Logo 2 as a fixed-position overlay,
 * animate it bouncing off the four viewport edges at constant velocity,
 * and detect a click. It does NOT touch state.js's advanceStage() or
 * know anything about the puzzle that comes next — it reports "clicked"
 * via a callback and lets the caller (main.js) decide what that means.
 * This keeps the module reusable/testable without dragging in the rest
 * of the sequence.
 * -----------------------------------------------------------------------
 */

import { STAGES, canActivate, getStage } from './state.js';

const LOGO_SRC = 'images/Will_Boone_Bouncing_Logo.png';
const LOGO_SIZE = 90; // px — fixed square render size, independent of source resolution
const SPEED = 220; // px/second — constant velocity magnitude

/**
 * Mounts the bouncing logo overlay and begins animating it.
 *
 * @param {Object} opts
 * @param {() => void} opts.onClicked - called once, synchronously, the
 *   moment the logo is clicked. The caller is responsible for advancing
 *   state and mounting the next stage — this module's job ends at
 *   detecting the click and cleaning itself up.
 * @returns {{ destroy: () => void } | null} a handle to forcibly tear
 *   down the overlay (used by main.js if discovery state changes out
 *   from under it), or null if this stage isn't eligible to run.
 */
export function mountBouncingLogo({ onClicked }) {
  // Self-gating: the bouncing logo is only the right thing to show
  // when we're still at IDLE. If the visitor already clicked it in a
  // previous page load (stage advanced past IDLE) but hasn't finished
  // later stages, main.js should be mounting the puzzle instead — this
  // module refuses to double-mount itself in that case.
  if (getStage() !== STAGES.IDLE) {
    return null;
  }

  const prefersReducedMotion = window.matchMedia(
    '(prefers-reduced-motion: reduce)'
  ).matches;

  const img = document.createElement('img');
  img.src = LOGO_SRC;
  img.alt = '';
  img.setAttribute('aria-hidden', 'true'); // decorative/hidden by design — this is an easter egg, not primary content
  img.className = 'ee-bouncing-logo';
  img.style.width = `${LOGO_SIZE}px`;
  img.style.height = `${LOGO_SIZE}px`;

  document.body.appendChild(img);

  let destroyed = false;
  let rafId = null;

  // Reduced-motion path: still fulfills "clickable object starts the
  // sequence," just without the constant-motion animation, per the
  // site's existing @media (prefers-reduced-motion: reduce) convention
  // (see index.html's global reduced-motion rule this respects the
  // same intent). The logo is placed in a fixed, visible spot instead.
  if (prefersReducedMotion) {
    img.style.position = 'fixed';
    img.style.left = '24px';
    img.style.top = '24px';
    img.style.cursor = 'pointer';
    img.addEventListener('click', handleClick, { once: true });
    return { destroy };
  }

  // Physics state. Position is the top-left corner of the image in
  // viewport coordinates. Velocity components are always ±SPEED —
  // constant magnitude, direction only changes on wall collision
  // (that's what "boundary reflection physics" means: reflect the
  // velocity component perpendicular to whichever wall was hit,
  // leave the other component untouched).
  //
  // Deliberate starting position/direction: near the top-left corner,
  // heading down-and-left. It's placed a short distance in from the
  // corner (not pinned to exactly x=0,y=0) so the initial leftward
  // travel is actually visible for a moment before the boundary
  // reflection below kicks in — starting exactly at x=0 with a
  // leftward velocity would bounce on the very first frame, making
  // "moving left" imperceptible.
  const START_INSET = 120; // px from each edge
  let x = START_INSET;
  let y = START_INSET * 0.4;
  let vx = -SPEED; // left
  let vy = SPEED; // down

  img.style.position = 'fixed';
  img.style.left = `${x}px`;
  img.style.top = `${y}px`;
  img.style.cursor = 'pointer';

  let lastTimestamp = null;

  function tick(timestamp) {
    if (destroyed) return;

    if (lastTimestamp === null) {
      lastTimestamp = timestamp;
    }
    // Delta-time based movement (not a fixed px-per-frame step) keeps
    // speed visually consistent regardless of the display's refresh
    // rate (60Hz vs 120Hz+ monitors) — a fixed-step approach would
    // make the logo move twice as fast on a 120Hz screen.
    const dt = (timestamp - lastTimestamp) / 1000;
    lastTimestamp = timestamp;

    x += vx * dt;
    y += vy * dt;

    const maxX = window.innerWidth - LOGO_SIZE;
    const maxY = window.innerHeight - LOGO_SIZE;

    // Boundary reflection: clamp position back inside the viewport and
    // flip the velocity component on whichever axis overshot. Clamping
    // (rather than just flipping velocity) prevents the logo from
    // drifting further out of bounds on a single slow frame before the
    // flip takes effect.
    if (x <= 0) {
      x = 0;
      vx = Math.abs(vx);
    } else if (x >= maxX) {
      x = maxX;
      vx = -Math.abs(vx);
    }

    if (y <= 0) {
      y = 0;
      vy = Math.abs(vy);
    } else if (y >= maxY) {
      y = maxY;
      vy = -Math.abs(vy);
    }

    // Deliberately never touching img.style.transform's rotate() —
    // per spec, orientation must remain fixed. Only translate via
    // left/top.
    img.style.left = `${x}px`;
    img.style.top = `${y}px`;

    rafId = requestAnimationFrame(tick);
  }

  rafId = requestAnimationFrame(tick);

  img.addEventListener('click', handleClick, { once: true });

  // Re-clamp position on resize so the logo can't end up stranded
  // outside a shrunk viewport (e.g. rotating a phone, or resizing a
  // desktop window mid-animation).
  function handleResize() {
    x = Math.min(x, window.innerWidth - LOGO_SIZE);
    y = Math.min(y, window.innerHeight - LOGO_SIZE);
  }
  window.addEventListener('resize', handleResize);

  function handleClick() {
    if (destroyed) return;
    onClicked();
    destroy();
  }

  function destroy() {
    if (destroyed) return;
    destroyed = true;
    if (rafId !== null) cancelAnimationFrame(rafId);
    window.removeEventListener('resize', handleResize);
    img.remove();
  }

  return { destroy };
}
