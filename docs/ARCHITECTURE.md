# How net-link works

## One message, step by step

1. The listener accepts a TCP socket. The server reserves one of ten slots immediately, before receiving a name.
2. A reader goroutine reads newline-delimited input with a size limit. A writer goroutine sends deliveries in queue order. Both belong to this connection.
3. A valid name joins `lobby` while the server mutex is locked. The server captures the current history length and profile, queues that replay, then announces the join to other members.
4. For a message, the reader locks the mutex, validates the text, writes its timestamped line to the room’s disk history, and writes the activity log. Only successful persistence increments counters.
5. The server places the same immutable string in each other room member’s queue. It never puts that message in the sender’s queue. It may put a separate private flood warning there.
6. The mutex unlocks. Every socket writer operates independently, with its own deadline. A blocked output queue closes only that peer’s socket. Closing unblocks its reader, which removes the peer and announces departure.

Two simultaneous senders compete for the same mutex. Whichever acquires it first is persisted and queued first. All recipients therefore see the same relative order. There is no claim about which sender wins a tie in real-world arrival time.

## Why history does not race with live traffic

A replay delivery contains an open history file, a fixed byte length, and the profile computed at that length. The writer reads only that prefix through `io.NewSectionReader`; it uses positional reads, not a shared seek position. Later history appends use `WriteAt` at the next committed byte offset. Live messages are queued after the replay delivery. The file may grow, but the replay boundary does not move, so the joiner cannot miss or repeat a line at the boundary.

A failed message is not counted or broadcast. The next successful write starts at the last committed offset, even if truncation of a failed append failed. Existing replay readers only cover committed prefixes. The two files are not a transactional database: a crash, partially failed OS write, or sudden power loss can leave an incomplete activity-log record. Old sessions remain available for inspection and are never silently treated as the next session’s counters.

The mutex includes synchronous file writes. This keeps the sequence easy to audit, at the cost of disk stalls delaying message acceptance. Socket stalls are isolated; unresponsive storage is not promised to be non-blocking.

## Profiler: arithmetic, not a model service

`Pace(12, 5)` computes 2.4 and returns `quiet`. `Pace(3, 1)` and `Pace(10, 1)` are `active`. `Pace(11, 1)` is `lively`. No rounding occurs before comparison.

`TopTalker` scans the count map once. A larger count wins; equal counts use the alphabetically smaller Go string. Map iteration order therefore cannot change the result. `Summary` only formats these helper results. Empty text, invalid text, commands, system notices and private warnings never change counters.

The flood detector retains at most six timestamps. On each accepted message it removes timestamps aged 30 seconds or more, appends the new time and checks whether more than five remain. This is a sliding window, not a timer that resets every wall-clock half-minute. The warning follows the sending connection across nickname and room changes. Only accepted chat text participates.

## Ownership and shutdown

- `Server.mu`: clients, rooms, counters, history size, output-queue order.
- `connection.stopOnce`: close socket and `done` exactly once.
- `Server.closeOnce`: stop accepts, close every socket, wait for readers/writers/accept loop, then close history files.
- `log.Logger`: serialize activity log writes, including input-error diagnostics.
- `client.Pump`: owns its connection and a closeable input stream; closing input cancels a pending read.
- TUI: only the gocui event loop touches views. The network reader acknowledges each `Gui.Update` before submitting the next update, because upstream explicitly does not guarantee ordering of independently posted updates. The outbound channel is bounded; typing cannot block on network I/O.

The TUI library itself manages its terminal polling goroutines; the UI is created once per client process. A terminal client that loses its connection remains open with a disconnect status so its visible conversation can still be read; Ctrl-C exits.

## Reading order

Start with `internal/chatprofile/profiler.go`, then `server/protocol.go`, `server/server.go`, `server/commands.go`, `client/client.go`, `main.go`, and finally `client/tui.go`. Tests explain boundary conditions using concrete transcripts. The protocol test helper uses a profile request as an ordered barrier, so “no echo” is checked by the next reply rather than an arbitrary sleep.

Suggested rehearsal: predict the next room transcript after two messages, a nickname change, a room change, and a reconnect. Then reproduce it using three `nc` terminals. Explain which goroutine owns each step before consulting this document.
