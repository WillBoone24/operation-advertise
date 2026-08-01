package game

// -----------------------------------------------------------------------
// The king's chambers are the true end of a run — reached only after
// BOTH the dungeon (Stages 1-5) and the Journey (Stages 6-10) are
// cleared (see state.go's SaveState.AtKingsChambers and stages.go's
// TotalStages doc comment). A player who reaches them is shown the
// Hall of Legacies (database.GetRecentLegacies — everyone who's
// finished the game before them, and which of these three paths they
// chose) and then picks exactly one, permanently, via handleChoosePath.
//
// Each path's ResultText is fixed, static content — not rolled, not
// randomized, not influenced by class/stats — deliberately: this is
// the character's ending, and every player who picks "House of Heroes"
// should be able to compare notes on the exact same paragraph, the
// same way a real game's fixed ending slides work.
// -----------------------------------------------------------------------

// LegacyPathID identifies one of the three endings. Stored as a
// string (like ClassID/Difficulty) for the same human-readable-save-
// file and human-readable-database-row reasoning both already follow.
type LegacyPathID string

const (
	LegacyPathLords   LegacyPathID = "lords"
	LegacyPathCommons LegacyPathID = "commons"
	LegacyPathHeroes  LegacyPathID = "heroes"
)

// LegacyPathInfo is one of the three endings: its display name, the
// short pitch shown while the player is still choosing, and the fixed
// narrative shown once they commit to it.
type LegacyPathInfo struct {
	ID   LegacyPathID `json:"id"`
	Name string       `json:"name"`

	// Pitch is the one-line summary shown in the chambers' menu,
	// before a choice is made — enough to decide by, not the ending
	// itself.
	Pitch string `json:"pitch"`

	// ResultText is the fixed, permanent narrative shown the moment
	// this path is chosen (handleChoosePath) — see this file's
	// package doc comment on why it's static rather than generated.
	ResultText string `json:"-"`
}

// LegacyPaths is the fixed set of three endings a completed run can
// choose from. Order here is display order in the chambers' menu, not
// meaningful otherwise — no ending is "better" than another, each is
// simply a different answer to what the character does with the rest
// of their life.
var LegacyPaths = []LegacyPathInfo{
	{
		ID:    LegacyPathLords,
		Name:  "House of Lords",
		Pitch: "Take a seat among the kingdom's nobility and rule.",
		ResultText: "The king rises before the full court and offers you a seat among the " +
			"House of Lords — land, title, and a voice in how the realm is governed. You " +
			"take it. Within a season your name is spoken in council chambers instead of " +
			"taverns. You never wield a blade in anger again, but the roads you once bled " +
			"for get safer under your signature, one decree at a time. Historians will " +
			"argue for generations about whether the dungeon or the council table asked " +
			"more of you.",
	},
	{
		ID:    LegacyPathCommons,
		Name:  "House of Commons",
		Pitch: "Go home. Build something with the people who never left.",
		ResultText: "You thank the king, and decline the title he offers. Power was never " +
			"the point — the people who kept this kingdom standing while you were gone " +
			"were. You return to the town nearest the dungeon's mouth, the one that grew " +
			"up around travelers like you, and you stay. You teach. You mend fences you " +
			"didn't break. You settle disputes over well water and property lines instead " +
			"of monsters. No statue is raised in your honor — but for the rest of your " +
			"life, half the town knows your name, and all of it sleeps easier for it.",
	},
	{
		ID:    LegacyPathHeroes,
		Name:  "House of Heroes",
		Pitch: "Keep the sword. The realm has other dark places.",
		ResultText: "You decline both land and quiet. The dungeon is cleared, the road home " +
			"is safe, but you both know there will be another dungeon, another valley no " +
			"one wants to walk into. The king inducts you into the House of Heroes instead " +
			"— not a rank so much as a promise, made in both directions, that when " +
			"something dark rises again, you'll already be moving toward it. You leave the " +
			"chambers the same way you entered your very first tunnel: alone, armed, and " +
			"already looking at the horizon.",
	},
}

// GetLegacyPath mirrors GetClass/GetFamiliar's (found, ok) lookup
// pattern — a path ID reaching handleChoosePath originates from
// client input (a "choose_path" action's item_id), not trusted server
// data, so the lookup needs a clean way to reject an unknown ID rather
// than panic.
func GetLegacyPath(id string) (LegacyPathInfo, bool) {
	for _, p := range LegacyPaths {
		if string(p.ID) == id {
			return p, true
		}
	}
	return LegacyPathInfo{}, false
}
