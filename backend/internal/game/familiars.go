package game

import (
	"fmt"
	"math/rand"
)

// -----------------------------------------------------------------------
// Familiars are found, not chosen — a character can hold exactly one
// at a time (SaveState.Familiar), picked entirely at random on a kill
// once FamiliarDropStage is reached, with no way to influence which of
// the seven shows up. See RollFamiliarDrop's doc comment for the drop
// rule itself; this file is the familiar DEFINITIONS plus the combat
// behavior each one grants.
//
// Every familiar takes its own small mini-turn each combat round,
// alongside (not instead of) the player's own action — see
// ResolveFamiliarAction, called once per round from
// handlers/game.go's shared combat-round helper whenever
// state.Familiar != "".
// -----------------------------------------------------------------------

// FamiliarKind is a closed set (mirroring SpecialCondition/PotionKind)
// so ResolveFamiliarAction can switch on a known list rather than
// interpreting arbitrary per-familiar behavior data.
type FamiliarKind string

const (
	FamiliarKindMirror  FamiliarKind = "mirror"   // reflects a portion of incoming damage back
	FamiliarKindLeech   FamiliarKind = "leech"    // small attack, heals the player for part of it
	FamiliarKindHex     FamiliarKind = "hex"      // small attack, also poisons the enemy
	FamiliarKindReaper  FamiliarKind = "reaper"   // small attack, big bonus if the enemy is already low
	FamiliarKindAegis   FamiliarKind = "aegis"    // chance to fully block the incoming counterattack
	FamiliarKindWisp    FamiliarKind = "wisp"     // random small effect each round
	FamiliarKindStormtail FamiliarKind = "stormtail" // small attack, chance to stun the enemy
)

// Familiar is one of the seven possible companions. DisplayFlavor is
// the "appears from the shadows" line shown at the moment it's found
// (see RollFamiliarDrop's caller in handlers/game.go's
// grantVictoryRewards) — kept on the data itself rather than
// hardcoded in the handler, same "content lives with its definition"
// convention as everything else in this package.
type Familiar struct {
	ID          string       `json:"id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Kind        FamiliarKind `json:"kind"`
	DisplayFlavor string     `json:"-"`
}

// Familiars is the fixed set of seven. None are elemental-sprite
// reskins of each other — each Kind resolves through genuinely
// different logic in ResolveFamiliarAction below, not just a
// different damage number on the same mechanic.
var Familiars = []Familiar{
	{
		ID: "f_mirror_ward", Name: "Mirror Ward", Kind: FamiliarKindMirror,
		Description:   "A drifting shard of polished obsidian. It doesn't attack — it answers.",
		DisplayFlavor: "A shard of dark glass drifts out of the shadows and settles at your shoulder. The Mirror Ward has chosen you.",
	},
	{
		ID: "f_leech", Name: "Bloodleech", Kind: FamiliarKindLeech,
		Description:   "A small, patient thing that takes a little from your enemies and gives it back to you.",
		DisplayFlavor: "Something small and patient slips from the dark and fastens itself near your collar. The Bloodleech has chosen you.",
	},
	{
		ID: "f_hex", Name: "Hexmoth", Kind: FamiliarKindHex,
		Description:   "Its wingbeats carry a fine, venomous dust.",
		DisplayFlavor: "A pale moth spirals out of the shadows, dust trailing from its wings. The Hexmoth has chosen you.",
	},
	{
		ID: "f_reaper", Name: "Little Reaper", Kind: FamiliarKindReaper,
		Description:   "Barely more than a shadow with a scythe. It knows exactly when a fight is already over.",
		DisplayFlavor: "A tiny shadow, scythe in hand, steps out from behind you as if it had always been there. The Little Reaper has chosen you.",
	},
	{
		ID: "f_aegis", Name: "Aegis Whelp", Kind: FamiliarKindAegis,
		Description:   "A small armored shape that throws itself in the way more often than it should.",
		DisplayFlavor: "A small, armored shape shoulders its way out of the shadows and plants itself protectively at your side. The Aegis Whelp has chosen you.",
	},
	{
		ID: "f_wisp", Name: "Wandering Wisp", Kind: FamiliarKindWisp,
		Description:   "Nobody — least of all the wisp — knows what it's going to do next.",
		DisplayFlavor: "A flickering, restless light spills out of the dark and orbits your head lazily. The Wandering Wisp has chosen you.",
	},
	{
		ID: "f_stormtail", Name: "Stormtail", Kind: FamiliarKindStormtail,
		Description:   "A crackling, restless shape that strikes fast enough to leave things reeling.",
		DisplayFlavor: "A crackling shape bounds out of the shadows, static snapping off its tail. Stormtail has chosen you.",
	},
}

// GetFamiliar mirrors GetClass/GetWeapon's (found, ok) lookup pattern.
func GetFamiliar(id string) (Familiar, bool) {
	for _, f := range Familiars {
		if f.ID == id {
			return f, true
		}
	}
	return Familiar{}, false
}

// FamiliarDropStage is the earliest CurrentStage a familiar can drop
// from — Stage 4, Part 1 onward (any part within Stage 4 or 5
// qualifies), per the design decision to hold familiars back until
// the back half of the run.
const FamiliarDropStage = 4

// FamiliarDropChancePercent is the flat, class-agnostic chance any
// single eligible kill drops a familiar — mirrors
// tavern.go's GoldDropChancePercent in shape (a flat rollPercent
// check), just a separate, lower number so a familiar stays a
// genuinely uncommon find rather than an every-few-fights guarantee.
const FamiliarDropChancePercent = 20

// RollFamiliarDrop decides whether a just-defeated enemy at
// currentStage >= FamiliarDropStage produces a familiar, but ONLY if
// hasFamiliar is false — a player who already holds one sees no drop
// at all, not a near-miss message, not a "you sense something" line.
// That suppression happens here, at the roll itself, specifically so
// the caller (grantVictoryRewards) never has anything drop-shaped to
// even consider showing when it's not eligible.
//
// Returns a random Familiars entry (id, true) on a successful roll,
// or ("", false) otherwise.
func RollFamiliarDrop(rng *rand.Rand, currentStage int, hasFamiliar bool) (string, bool) {
	if hasFamiliar || currentStage < FamiliarDropStage {
		return "", false
	}
	if rollPercent(rng) > FamiliarDropChancePercent {
		return "", false
	}
	chosen := Familiars[rng.Intn(len(Familiars))]
	return chosen.ID, true
}

// GrantSecondFamiliar is the guaranteed (non-random) companion moment
// at the Journey's Ancient Woods finale — see stages.go's
// AncientWoodsFamiliarStage/Part and handlers/game.go's handleDescend,
// which calls this exactly once, right after that specific encounter
// is cleared.
//
// Unlike RollFamiliarDrop, this never fails to grant SOMETHING:
//   - If the character never lucked into a primary familiar via the
//     ordinary random drop (RollFamiliarDrop's 20%-per-eligible-kill
//     roll can simply whiff for an entire run), this fills the
//     PRIMARY slot instead of the secondary one — a character who
//     reaches this story beat with zero companions shouldn't leave it
//     with zero companions just because the mechanic meant for their
//     second one assumes they already have a first.
//   - Otherwise it fills SecondFamiliar with a random pick that is
//     explicitly NOT the same familiar already bonded in Familiar —
//     two identical companions would just double one Kind's numbers
//     rather than giving the player a second, distinct kit.
//
// Returns the ID of whichever familiar was actually granted, and
// which field it landed in (primary vs secondary) so the caller can
// phrase the narration correctly.
func GrantSecondFamiliar(rng *rand.Rand, state *SaveState) (familiarID string, wasPrimary bool) {
	if state.Familiar == "" {
		chosen := Familiars[rng.Intn(len(Familiars))]
		state.Familiar = chosen.ID
		return chosen.ID, true
	}

	if state.SecondFamiliar != "" {
		// Already has both — nothing to grant. Shouldn't happen given
		// this is a one-time trigger on a specific encounter, but
		// guarded defensively rather than silently overwriting an
		// existing bond.
		return "", false
	}

	// Pick uniformly among every familiar EXCEPT the one already
	// bonded, so the two companions are always mechanically distinct.
	var candidates []Familiar
	for _, f := range Familiars {
		if f.ID != state.Familiar {
			candidates = append(candidates, f)
		}
	}
	chosen := candidates[rng.Intn(len(candidates))]
	state.SecondFamiliar = chosen.ID
	return chosen.ID, false
}

// FamiliarActionResult is the fully-resolved outcome of one familiar
// mini-turn — pure data, same "combat.go produces numbers, the
// handler renders them" split as AttackResult.
type FamiliarActionResult struct {
	DamageToEnemy int
	HealToPlayer  int

	// BlockNextHit, if true, means the enemy's counterattack THIS
	// round should be fully negated (Aegis Whelp, and one of Wandering
	// Wisp's random outcomes) — read and consumed by
	// resolveEnemyCounterattack, not persisted between rounds.
	BlockNextHit bool

	// ApplyPoison/ApplyBurn/ApplyStun reuse the exact same ailment
	// system a weapon's SpecialCondition can trigger (items.go) — a
	// familiar landing a hex is mechanically identical to a poisoned
	// weapon landing one, just from a different source.
	ApplyPoison bool
	ApplyBurn   bool
	ApplyStun   bool

	Lines []string
}

// ResolveFamiliarAction runs the bonded familiar's mini-turn for one
// combat round, mirroring ResolveAttack's role for a weapon swing:
// deterministic given the same *rand.Rand, no I/O, no SaveState
// mutation — the caller (handlers/game.go) is what actually applies
// DamageToEnemy/HealToPlayer/ailments to InCombat/CurrentHP.
//
// enemyCurrentHP/enemyMaxHP are needed for the Little Reaper's
// execute-style bonus; playerName is purely for the log line's
// phrasing.
func ResolveFamiliarAction(rng *rand.Rand, familiarID string, enemyName string, enemyCurrentHP, enemyMaxHP int) FamiliarActionResult {
	familiar, ok := GetFamiliar(familiarID)
	if !ok {
		return FamiliarActionResult{}
	}

	switch familiar.Kind {
	case FamiliarKindMirror:
		// Passive by design — Mirror Ward does nothing on the
		// familiar's own mini-turn; its effect only fires in
		// resolveEnemyCounterattack, reflecting a slice of whatever
		// damage the enemy just dealt to the player back onto the
		// enemy. Nothing to resolve here.
		return FamiliarActionResult{}

	case FamiliarKindLeech:
		dmg := 1 + rng.Intn(4) // 1d4
		heal := dmg / 2
		if heal < 1 {
			heal = 1
		}
		return FamiliarActionResult{
			DamageToEnemy: dmg,
			HealToPlayer:  heal,
			Lines:         []string{fmt.Sprintf("Your Bloodleech darts in for %d damage and drains %d HP back to you.", dmg, heal)},
		}

	case FamiliarKindHex:
		dmg := 1 + rng.Intn(3) // 1d3
		return FamiliarActionResult{
			DamageToEnemy: dmg,
			ApplyPoison:   true,
			Lines:         []string{fmt.Sprintf("Your Hexmoth stings %s for %d damage, trailing venomous dust.", enemyName, dmg)},
		}

	case FamiliarKindReaper:
		dmg := 1 + rng.Intn(3) // 1d3
		remainingAfter := enemyCurrentHP - dmg
		if enemyMaxHP > 0 && remainingAfter > 0 && remainingAfter*4 <= enemyMaxHP {
			bonus := 6
			dmg += bonus
			return FamiliarActionResult{
				DamageToEnemy: dmg,
				Lines:         []string{fmt.Sprintf("Your Little Reaper senses the end is near and strikes for %d damage!", dmg)},
			}
		}
		return FamiliarActionResult{
			DamageToEnemy: dmg,
			Lines:         []string{fmt.Sprintf("Your Little Reaper takes a small cut at %s for %d damage.", enemyName, dmg)},
		}

	case FamiliarKindAegis:
		if rollPercent(rng) <= 25 {
			return FamiliarActionResult{
				BlockNextHit: true,
				Lines:        []string{"Your Aegis Whelp throws itself in front of the incoming blow!"},
			}
		}
		return FamiliarActionResult{}

	case FamiliarKindWisp:
		switch rng.Intn(3) {
		case 0:
			heal := 1 + rng.Intn(3)
			return FamiliarActionResult{
				HealToPlayer: heal,
				Lines:        []string{fmt.Sprintf("Your Wandering Wisp flickers warmly, restoring %d HP.", heal)},
			}
		case 1:
			return FamiliarActionResult{
				ApplyBurn: true,
				Lines:     []string{fmt.Sprintf("Your Wandering Wisp flares hot against %s, searing it.", enemyName)},
			}
		default:
			return FamiliarActionResult{
				BlockNextHit: true,
				Lines:        []string{"Your Wandering Wisp darts protectively in front of you."},
			}
		}

	case FamiliarKindStormtail:
		dmg := 1 + rng.Intn(3) // 1d3
		applyStun := rollPercent(rng) <= 25
		lines := []string{fmt.Sprintf("Your Stormtail lashes %s for %d damage.", enemyName, dmg)}
		if applyStun {
			lines = append(lines, "The strike leaves it reeling, stunned!")
		}
		return FamiliarActionResult{
			DamageToEnemy: dmg,
			ApplyStun:     applyStun,
			Lines:         lines,
		}

	default:
		return FamiliarActionResult{}
	}
}