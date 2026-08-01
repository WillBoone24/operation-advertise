/**
 * statusLine.js
 * -----------------------------------------------------------------------
 * Renders a compact status readout from GameState: an HP bar, current
 * dungeon stage, and (if applicable) the enemy currently being fought.
 *
 * Called after every command that can change game state — attack,
 * equip, descend, create — so the player always has a persistent read
 * on where they stand without needing to separately run "look" or
 * "inventory" to find out. This is the fix for exactly the "ran out
 * of options after one attack" problem: previously, an action's
 * result was a single log line with no ambient state shown alongside
 * it.
 *
 * NOT called by look.js/inventory.js on top of their own output —
 * look.js calls it directly as its dashboard, but doesn't duplicate it
 * with a second narrative HP mention; inventory.js likewise leads with
 * it once, not per-item.
 * -----------------------------------------------------------------------
 */

// REGION_NAMES labels Journey stages (6-10) for display — see
// stages.go's Journey doc comment for the region each stage number
// corresponds to. Stages 1-5 stay labeled by number alone ("Dungeon
// Stage N/5"); this is display-only content, same "kept in sync by
// hand" tradeoff tavern.js's SHOP_ITEMS already accepts.
const REGION_NAMES = {
  6: 'Greenwood Trail',
  7: 'Rolling Plains',
  8: 'Ancient Woods',
  9: 'Shadow Valley',
  10: 'Black Mire',
};

/**
 * Renders "Dungeon Stage N/5" for stages 1-5, or "Journey — <Region>
 * (N/5)" for stages 6-10 — the two halves of the run are numbered
 * independently in the player-facing text even though they share one
 * CurrentStage counter server-side (see stages.go's TotalStages=10
 * doc comment on why).
 */
function formatStageLabel(stage) {
  if (stage <= 5) {
    return `Dungeon Stage ${stage}/5`;
  }
  const region = REGION_NAMES[stage] || `Region ${stage - 5}`;
  return `Journey — ${region} (${stage - 5}/5)`;
}

export function printStatus(term, gameState) {
  if (!gameState.hasCharacter) return;

  if (gameState.runComplete) {
    if (gameState.legacyPath) {
      term.print(`--- Your legacy is sealed: ${gameState.legacyPathName}. The game is over. ---`, 'system');
    } else {
      term.print('--- Your journey is complete. Type "hall" to enter the king\'s chambers. ---', 'system');
    }
    term.print(`Gold ${gameState.gold}`, 'system');
    return;
  }

  const hpBar = renderBar(gameState.currentHP, gameState.maxHP);
  term.print(
    `HP ${hpBar} ${gameState.currentHP}/${gameState.maxHP}   ${formatStageLabel(gameState.currentStage)}`,
    'system'
  );

  // STR/DEX/CON and death marks are always shown alongside HP now —
  // previously only the HP line printed here, which left the two
  // other things that can end a run (a bad stat roll going unnoticed,
  // or racking up death marks toward the 24h lockout) invisible until
  // the player went looking for them.
  if (gameState.stats) {
    term.print(
      `STR ${gameState.stats.str}   DEX ${gameState.stats.dex}   CON ${gameState.stats.con}   Death Marks ${gameState.deathMarks}/5`,
      'system'
    );
  }

  // Gold is always shown, same "always relevant, unlike Mana" reason
  // it's never omitted on the wire (see gameStateResponse.Gold).
  term.print(`Gold ${gameState.gold}`, 'system');

  // Mana only means anything to a Mage — every other class's mana
  // stays 0 and unused, so it stays hidden rather than showing a
  // permanently-zero line to classes that can never cast.
  if (gameState.class === 'mage') {
    term.print(`Mana ${gameState.mana}`, 'system');
  }

  // Ability shows up for every class except Mage (no separate
  // "ability" action — cast.js's spell list already covers Firebolt)
  // and Rogue (Sneak Attack is passive, nothing to ever show as
  // "usable"/"used" here). Fighter/Cleric/Ranger see whether their
  // once-per-stage ability is still available, same "ambient
  // dashboard reminder" role Familiar/Mana play above.
  if (gameState.ability && gameState.class !== 'mage' && gameState.class !== 'rogue') {
    const availability = gameState.ability.usable ? 'available' : 'used this stage';
    term.print(`${gameState.ability.name}: ${availability}`, 'system');
  }

  // Familiar shows up here once bonded (Stage 4+ onward), same
  // "always visible on the persistent dashboard, not just when asked"
  // treatment as Gold/Mana above — the "familiar" command is for the
  // full description, this line is just the ambient reminder it's
  // there. Nothing prints in its place before one is found, same as
  // Mana staying hidden for non-Mages rather than showing an empty
  // placeholder.
  if (gameState.familiar) {
    term.print(`Familiar: ${gameState.familiar.name}`, 'system');
  }
  // A second familiar only ever appears once the first slot is
  // already filled (see familiars.go's GrantSecondFamiliar), so this
  // never shows on its own.
  if (gameState.secondFamiliar) {
    term.print(`Familiar: ${gameState.secondFamiliar.name}`, 'system');
  }

  if (gameState.atTavern) {
    if (gameState.dungeonComplete) {
      term.print('You are in the tavern. Type "tavern exit" when you\'re ready to begin the journey home.', 'system');
    } else {
      term.print('You are in the tavern. Type "tavern" to see what you can do here.', 'system');
    }
    return;
  }

  if (gameState.inCombat && gameState.inCombat.enemies) {
    // Simultaneous multi-enemy fight (currently just the Stage
    // 10/Part 3 ambush) — one HP bar per combatant, dead ones marked
    // instead of hidden so the player can see the fight's overall
    // progress, not just who's still standing.
    for (const enemy of gameState.inCombat.enemies) {
      if (enemy.currentHP <= 0) {
        term.print(`${enemy.name} — defeated`, 'system');
        continue;
      }
      const enemyBar = renderBar(enemy.currentHP, enemy.maxHP);
      term.print(`${enemy.name} (${enemy.id}) ${enemyBar} ${enemy.currentHP}/${enemy.maxHP}`, 'system');
    }
  } else if (gameState.inCombat) {
    const enemy = gameState.inCombat;
    const enemyBar = renderBar(enemy.enemyCurrentHP, enemy.enemyMaxHP);
    term.print(`${enemy.enemyName} ${enemyBar} ${enemy.enemyCurrentHP}/${enemy.enemyMaxHP}`, 'system');
  } else if (gameState.pendingAdvance) {
    term.print('Encounter cleared. Type "descend" to continue.', 'system');
  }
}

/**
 * Renders a fixed-width ASCII bar, e.g. "[######----]". Floors at an
 * empty bar rather than going negative-width if current somehow ends
 * up below 0 or above max — defensive only, current/max should always
 * be in range given the backend's own clamping in handleAttack.
 */
function renderBar(current, max, width = 10) {
  if (max <= 0) return '[' + ' '.repeat(width) + ']';
  const filled = Math.max(0, Math.min(width, Math.round((current / max) * width)));
  return '[' + '#'.repeat(filled) + '-'.repeat(width - filled) + ']';
}