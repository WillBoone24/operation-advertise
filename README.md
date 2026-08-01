# Operation Advertise

A resume site... with a secret. Buried in the page is a hidden puzzle — solve
it, and it unlocks a full terminal-based dungeon-crawler RPG running underneath
the portfolio.

## What's in here

- **`resume-frontend/`** — the public-facing portfolio/resume site, including
  the hidden easter-egg puzzle that unlocks the game.
- **`rpg-frontend/`** — a browser-based terminal emulator and the RPG's
  command-driven frontend (attack, cast, equip, tavern, etc.).
- **`backend/`** — a Go API server: JWT auth, bcrypt password hashing, SQLite
  storage, and all game logic (combat, spells, classes, dungeon stages, and
  the tavern's games of chance).

## The game

Create a character as one of five classes — **Fighter**, **Rogue**, **Mage**,
**Cleric**, or **Ranger** — and descend through a 10-stage run (Stages 1-5 are
the dungeon proper; 6-10 continue past it). Along the way:

- Turn-based combat with class-specific abilities and spells
- A tavern to rest at between stages — buy potions and scrolls, learn
  monster lore, solve a riddle, or gamble your gold at **blackjack** and
  **roulette** (both wagered and paid out in gold only, so no class has an
  edge)
- Familiars, legacy paths, and a persistent character save

It's played entirely as terminal commands (`attack`, `cast fireball`,
`tavern blackjack 5`, `descend`, etc.) — type `help` in-game for the full
command list.

## Running it locally

Three servers need to run at once: the Go backend, the resume-frontend, and
the rpg-frontend.

```bash
export JWT_SECRET="$(openssl rand -base64 48)"   # must be >= 32 bytes
cd backend && go run ./cmd/server
```

Then serve `resume-frontend/` and `rpg-frontend/` as static sites (any static
file server works — no build step). If you're on WSL2 where `localhost`
forwarding can be unreliable, `scripts/dev-up.sh` handles all three servers
for you (detects your WSL IP, regenerates each frontend's `config.js`, and
starts everything detached):

```bash
./scripts/dev-up.sh    # start everything
./scripts/dev-down.sh  # stop everything
```

### Backend API

| Method | Path              | Auth | Description                        |
|--------|-------------------|------|-------------------------------------|
| GET    | /health           | no   | liveness check                      |
| POST   | /api/register     | no   | create account, returns JWT         |
| POST   | /api/login        | no   | authenticate, returns JWT           |
| GET    | /api/me           | yes  | current user's public profile       |
| POST   | /api/easteregg    | yes  | mark easter egg found (idempotent)  |
| POST   | /api/game/action  | yes  | perform a game action               |

Send the JWT as `Authorization: Bearer <token>`.

### Build / test

```bash
cd backend
go build ./...
go vet ./...
```

## License

The source code in this repository — everything under `backend/`,
`rpg-frontend/`, and `resume-frontend/` **excluding** the files listed below —
is licensed under the [MIT License](./LICENSE).

**Not covered by the MIT license:**
- `resume-frontend/resume.pdf`
- `resume-frontend/images/Will_Boone_Logo.png` and
  `Will_Boone_Bouncing_Logo.png`

These are personal assets (my resume and personal branding) and are not
licensed for reuse or redistribution.
