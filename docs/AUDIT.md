# Audit map

The local assignment is the source of requirements. Its coaching prompts are educational text; the user explicitly requested a completed implementation. The project documents actual behavior and does not claim that automated tests certify the learner’s personal understanding.

| Subject requirement | Implementation | Evidence |
|---|---|---|
| Required layout and Go module | `main.go`, `server/`, `client/`, `internal/chatprofile/` | `go test ./...`, import script |
| Default 8989 / explicit 2525 / exact usage | `parse`, `run` | Go argument tests; real process checks |
| Linux logo and required name | `Welcome`, `read` | Real TCP greetings; empty/reserved/duplicate-name tests |
| At most ten clients | Slot reserved before handshake | Ten unnamed sockets plus rejected eleventh; slot reuse |
| Nonempty timestamped messages | `message` | Exact timestamp test; UTF-8 black-box test |
| No server self-echo | `broadcast` excludes sender | Ordered barrier test; eight concurrent sender transcripts |
| Join/leave announcements | `event` | Two-, three- and four-client tests; disconnect survivors |
| All prior messages | Disk snapshot delivery | Complete 600-message Go replay; 800-message black-box replay; 7.6 MB live/replay boundary |
| Goroutines and synchronization | Reader/writer goroutines, mutex, bounded channels | `go test -race`; race-instrumented binary audit in CI |
| Server and client errors | Deadline, queue, connection, persistence paths | Refusal, EOF, broken output, closed history, injected full disk, bad startup log |
| Dedicated profiler helpers | `Pace`, `TopTalker`, `Summary` | 100% statement coverage for profiler in local unit run |
| Immediate session profile | Same delivery as history | Exact empty and populated profile order tests |
| Fractional pace and 3/10 boundaries | Real division, no rounding | Unit table plus fuzzing |
| Name changes and announcements | `/nick` | Credits preserved, room informed, real TUI rename |
| Multiple chat groups | `/join`, `/rooms`, `/who` | Isolated histories/counters, room limit and invalid-name tests |
| More NetCat flags | `-l -p -s -c -v -w -N -z` | Parser tests; probe, refusal, genuine half-close/reply tests |
| Terminal UI | `github.com/jroimartin/gocui` | Real PTY enters name, sends text, renames, shrinks/resizes with draft preservation, exits |
| Saved logs | Activity log + per-room history files | Disk content assertions; failed-persistence rollback |
| Private flood warning | Last-six timestamp window | Exact sixth message, private-only delivery, expiration boundary |

## Repeatable checks

```sh
go vet ./...
go test -race -cover ./...
python3 scripts/check_imports.py
go build -race -o TCPChat .
python3 scripts/audit.py --stress --tui
go test ./internal/chatprofile -run '^$' -fuzz FuzzPace -fuzztime 10s -parallel 2
go test ./server -run '^$' -fuzz FuzzValidation -fuzztime 10s -parallel 2
go test . -run '^$' -fuzz FuzzArguments -fuzztime 10s -parallel 2
```

`testing` is used only in test files, as required for Go unit tests. Production direct imports are restricted to the assignment’s standard-library list, the project’s own packages, and the explicitly allowed gocui bonus. `termbox-go` and `go-runewidth` are gocui’s indirect dependencies, pinned in `go.mod`/`go.sum`; there is no second UI framework. Python is independent verification tooling.

## Scope and interpretation

The audit includes one three-client question asking whether all three receive a message, while the explicit behavior note and earlier test prohibit server echo to the sender. We preserve one local copy and deliver the same formatted message to both other clients. A later join receives all historical messages, regardless of who originally sent them.

Profiler “session” means the current server lifetime for the default room. Bonus rooms each start their own session at creation, avoiding disclosure of another room’s conversation or activity. Chat history contains chat messages; transient join/leave/rename notices are saved in the activity log, not replayed as messages.

Limits are explicit: ten concurrent sockets, 32 session rooms, 64-byte names, 4096-byte text, 256 queued deliveries per peer, 30-second name entry, five-second writes, and a 2000-line TUI window. Activity/history files are retained; storage capacity and the number of historical participant names are not artificially truncated. This is a plain TCP chat, not a durable authenticated messaging service.

The school’s linked `good-practices` endpoint could not be retrieved during implementation. The project applies Go formatting, small named helpers, explicit ownership, no panics for expected input failures, checked startup errors, bounded network buffers and independent tests. It does not claim to have verified an inaccessible checklist.

The oral stakeholder audit remains a live human exercise. Use the architecture walkthrough to study and demonstrate concurrency, locking, pace calculation, ties and examples; no test can establish that understanding for you.
