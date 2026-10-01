# net-link

[![Audit](https://github.com/ernat-soltanbekov/net-link/actions/workflows/ci.yml/badge.svg)](https://github.com/ernat-soltanbekov/net-link/actions/workflows/ci.yml)

A Go TCP group chat with complete session history, activity profiling, a NetCat-compatible protocol and a terminal client. Built for the Tomorrow School / 01-Edu `net-link` subject.

**Yernat “GhostOfAstana” Soltanbekov** — veteran, Tomorrow School student, technically educated, practicing martial arts every morning since 2008. Candidate for the Astana Hub team (МИИЦР РК), as described by the author; this personal learning project does not represent an appointment or institutional endorsement. The olive/green terminal identity and the `dojo` example reflect discipline, steady practice and clear communication.

## Start in three terminals

Requires **Go 1.25+**, macOS or Linux, and internet access for the first dependency download. Python 3 is used only by the audit scripts. The application is Go; its only direct external dependency is the explicitly permitted `gocui` TUI library.

```sh
git clone https://github.com/ernat-soltanbekov/net-link.git
cd net-link
go build -o TCPChat .
./TCPChat 2525
```

In the second and third terminals:

```sh
nc localhost 2525
# Or use the included clients:
./TCPChat -c localhost 2525
./TCPChat --tui localhost 2525
```

Enter a name when prompted, then type messages. Press Ctrl-C to exit. TUI: Enter sends; PgUp/PgDn scroll vertically; F7/F8 scroll horizontally; Ctrl-C exits. It shows one local `> message` line; incoming messages retain their timestamp and sender. A real terminal of at least 36×12 is required. Long incoming lines can be inspected with F7/F8 scrolling; plain `nc` also receives their full text.

`./TCPChat` listens on **8989**. `./TCPChat 2525 localhost` prints exactly:

```text
[USAGE]: ./TCPChat $port
```

The default bind address accepts connections on available interfaces. For a local-only session:

```sh
./TCPChat -l -p 2525 -s 127.0.0.1
./TCPChat -c -z -v -w 2 localhost 2525
./TCPChat -c -N localhost 2525
```

`-l` listens, `-p` selects a listening port, `-s` selects the local bind address, `-v` writes diagnostics to stderr, `-w` sets a **connection timeout** (not an idle timeout), `-z` probes a TCP port without sending input. `-N` half-closes output at input EOF and continues reading until the peer closes; without it, EOF closes the client. Run `./TCPChat --help` for all options. These are the implemented NetCat-style flags; UDP and arbitrary NetCat flag combinations are outside this TCP chat protocol.

## Commands and bonuses

| Command | Result |
|---|---|
| `/nick New Name` | Rename; notify the room and move this connection’s message credits to the new name. |
| `/join dojo` | Create/join an isolated room; receive its complete history and profile. |
| `/rooms` | List rooms and current membership. |
| `/who` | List members of the current room. |
| `/profile` | Recompute the current room’s activity summary. |
| `/help` | Show commands. |
| `/quit` | Disconnect this client. |

All requested bonuses are implemented: name changes and announcements, separate rooms, additional NetCat flags, a `gocui` UI, persistent activity logs, and a **private** warning after more than five messages in a sliding 30-second window. Warnings do not block messages or change message counters.

Names are unique among connected clients, case-insensitively; 1–64 UTF-8 bytes. Empty names, `System`, control characters and `[]|:/\` are rejected. Room names use 1–32 lowercase ASCII letters, digits, `-` or `_`. A maximum of 32 rooms is retained per server session. Empty rooms retain history for returning users. Lines starting with `/` are commands; unknown commands receive a private usage response.

## Profiling

`internal/chatprofile` exposes `Pace(total, elapsedMinutes)`, `TopTalker(counts)` and `Summary(...)`. Every room has its own start time and counters. The lobby starts with the server. Counters include successfully persisted chat messages only, including messages sent when nobody else is present.

| Real messages/minute | Label |
|---|---|
| Below 3 (including 2.4) | `quiet` |
| 3 through 10, inclusive | `active` |
| Above 10 | `lively` |

Immediately after history, a new participant receives:

```text
[System]: Session profile | pace: active | top talker: Yenlik (8 msgs) | total: 15 msgs
```

Without messages: `[System]: Session profile | pace: quiet | no messages yet`.

Ties use ascending names for deterministic results. Historical counts remain after disconnect; reconnecting with the same spelling adds to that name’s existing count. Renaming moves only that connection’s contributions, across rooms it visited, without changing total counts or historical message text. With positive messages but zero elapsed time, pace is `lively`; with zero messages it is always `quiet`. Tests cover fractional rates, both boundaries, ties and invalid helper inputs.

## Reliability and storage

- Ten connections total across all rooms, **including clients still entering a name**. The eleventh receives a refusal and closes. A pending name has a 30-second deadline.
- Separate reader/writer goroutines per connection. One mutex orders membership, counters, history append and queue insertion. Socket I/O never runs under that mutex. Disk writes are serialized with state changes.
- A 256-delivery queue per client, a five-second deadline per socket write, and a 4096-byte maximum message. A non-reading client loses its own connection; queue overflow never blocks a broadcaster. Oversized input disconnects; invalid control characters receive a private rejection.
- History is a disk file, replayed through a fixed-length reader. Messages accepted during replay queue behind that snapshot and its profile. Message history does not accumulate in server RAM. Counters retain one entry per historical name that still owns messages.
- `logs/net-link.log` contains activity and accepted messages; `logs/session-*/ROOM.history` contains complete, timestamped room transcripts. These are private local files, ignored by Git. `--log PATH` changes the activity log path; its parent directory must exist. Every restart begins fresh room counters/history while retaining earlier files and appending to the activity log.
- A failed history or message-log write prevents broadcast and counting, rolls back the history prefix where possible, and tells the sender the message was not delivered. Successful writes are buffered by the OS; power-loss durability and cross-file crash transactions are not claimed. Runtime system-event log failures are reported to stderr.
- Files are retained without automatic deletion. Disk usage grows with accepted traffic; remove old session files only when the server is stopped. The TUI retains the most recent 2000 screen lines; the server transcript and raw TCP replay remain complete.

The protocol is plain TCP with user-chosen names; it does not provide account authentication, encryption, or message delivery acknowledgements. A local typed line is not a delivery receipt. `Server.Close()` is idempotent and waits for its owned goroutines. The standalone process uses normal OS signal termination; logs are written per event without a user-space buffering layer.

## Verify and study

```sh
go test ./...
make test                 # vet, race detector, import allowlist
make audit                # black-box acceptance + real TUI through a PTY
make stress               # also 800 concurrent messages and ~7.6 MB replay
```

The black-box tests temporarily use ports 8989/2525 for the exact audit scenarios, then ephemeral ports. Those two ports must be free. The CI runs the full suite on Linux (Go 1.25 and 1.27) and macOS (Go 1.27), with fuzzing on the latest Linux job.

Read [the audit checklist](docs/AUDIT.md) and [the concurrency walkthrough](docs/ARCHITECTURE.md). The written subject contradicts itself once about echoing to the sender; this implementation follows its explicit **no self-echo** requirement. Automated checks do not replace the audit’s conversation about your understanding.
