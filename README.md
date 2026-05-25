# graft: Backups Done Right

### graft is a Git backup/versioning utility based on Git submodules

Have you ever wished you could back up your `nginx/sites-available` folder and do it simple and quick? Tinker with your configs without breaking everything in process? Store and version your configs on a Git platform? graft is here to help!

<img width="1200" alt="graft-init" src="https://github.com/user-attachments/assets/7fe175bd-4c62-4074-966f-98de78f29c7f" />
<br>
<br>

Here's how it's done:

0. Create and configure your Access Token ([here for GitHub](https://github.com/settings/tokens))

1. Initialize the repository:

`graft init git@github.com:user/backup-repo.git`

2. Put your token in `.config/graft.toml`:

<img width="536" height="106" alt="image" src="https://github.com/user-attachments/assets/226eabd3-c2ae-47b1-9c0b-daf418d2ae3a" />
<br>
<br>

3. Add any directory you desire as a blob:

<img width="1200" alt="graft-niri" src="https://github.com/user-attachments/assets/7edd6fa6-0e53-46c9-a525-76c434e86c53" />
<br>
<br>

...even if it's root owned!

<img width="612" height="202" alt="image" src="https://github.com/user-attachments/assets/81072603-c476-455b-af3f-bf5787615914" />
<br>
<br>

4. Watch your blobs appear in the repo config:

<img width="557" height="185" alt="image" src="https://github.com/user-attachments/assets/e7fd261c-34d2-4072-b30e-bcfc9cdd56ad" />
<br>
<br>

5. Sync your configs...

<img width="557" height="185" alt="image" src="https://github.com/user-attachments/assets/18216a8b-b2de-40b6-bec3-40df7a1a2c1b" />
<br>
<br>
6. Clone the repo on a new machine...

<img width="745" height="171" alt="image" src="https://github.com/user-attachments/assets/54ce7245-efaa-4e31-a4b5-02fbddb590c3" />
<br>
<br>

7. Restore your configuration!

<img width="391" height="162" alt="image" src="https://github.com/user-attachments/assets/8d4bedb9-deab-4a55-a29d-266138d33c01" />
<br>
<br>

graft is distributed as a single Go binary, but you can also build it yourself: `make install`.

---

## Commands

| Command | Description |
|---|---|
| `graft init <remote>` | Initialise main repo and config |
| `graft this <name> [--sudo] [--public]` | Start tracking current directory as blob |
| `graft here [name]` | Clone existing blob into current directory |
| `graft apply [--force] [name]` | Restore blob(s) to paths from config |
| `graft push [name]` | Commit and push blob(s) |
| `graft pull [--force] [name]` | Pull updates for blob(s) |
| `graft remove <name>` | Remove blob from tracking |
| `graft switch <name>` | Switch active repo |
| `graft repo add <remote>` | Clone and register a remote graft repo |
| `graft repo remove <name>` | Remove a repo from config |
| `graft repo list` | List all registered repos |

## Multiple repos

graft supports multiple repos — useful for testing someone else's dotfiles without touching your own.

```bash
# register and clone a remote graft repo
graft repo add git@github.com:user/their-dotfiles.git

# see what's registered
graft repo list

# switch to it
graft switch their-dotfiles

# restore their blobs
graft apply --force

# switch back
graft switch master
graft apply
```

Active repo is shown in all command output: `[master] push summary:`.

Default visibility for new blobs is set during `graft init` and stored in `graft.toml` as `public = false`. Override per-blob with `--public`.

## Blob flags

Flags are set in `graft.toml` after the path:

```toml
[blobs]
nvim    = "~/.config/nvim"
waybar  = "/etc/xdg/waybar sudo immutable"
```

| Flag | Description |
|---|---|
| `sudo` | Directory is root-owned; graft uses sudo for `mkdir`-ing/`chown`-ing |
| `immutable` | Path cannot be reassigned via `graft here` |

---

## Disclaimer

graft is a Git wrapper. It doesn't encrypt, modify or filter any of your data. Treat the remote as fully trusted — graft assumes it always is.
